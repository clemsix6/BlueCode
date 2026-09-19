using Compiler.Lexing;
using Compiler.Syntax;

namespace Compiler.Semantics;

// Walks the tree once more, this time asking what every name means and what type every
// expression has. Two passes: first every struct and function is collected into the global
// scope, so that a body may use anything declared anywhere in the file, then each body is
// checked. What is found is kept in tables keyed by node, for the code generator.
public class TypeChecker
{
    private readonly ProgramNode program;
    private readonly List<Diagnostic> diagnostics = [];
    private readonly Scope global = Scope.CreateGlobal();

    // The scope of the block being checked, and the function it belongs to.
    private Scope scope;
    private FunctionSymbol? currentFunction;

    // Records compare by value, so two "Name x" nodes would be the same key: compare by instance.
    private readonly Dictionary<Expression, Type> types = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<Expression, Symbol> symbols = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<FunctionDeclaration, FunctionSymbol> functions = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<StructDeclaration, StructType> structs = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<TypedName, VariableSymbol> variables = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<CatchBlock, VariableSymbol> catchVariables = new(ReferenceEqualityComparer.Instance);
    private readonly List<VariableSymbol> states = [];

    // The errors have their own namespace, "error.Frozen": no clash with a struct or a variable.
    private readonly Dictionary<string, ErrorSymbol> errorsByName = new();
    private readonly List<ErrorSymbol> errors = [];

    // The variables whose value was moved away on the path being checked, and where. A moved
    // variable is dead until it is assigned again. Depth is how deeply nested the block is, to
    // tell a variable of the loop body from one declared around the loop.
    private HashSet<VariableSymbol> moved = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<VariableSymbol, Token> moveSites = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<VariableSymbol, int> depths = new(ReferenceEqualityComparer.Instance);
    private int depth;

    // The places ref variables of the open blocks are bound to. While a ref points into a
    // place, that place is frozen: not assigned, moved, taken or passed by ref, so that what the
    // ref names stays alive and stays what it was.
    private readonly record struct Borrow(VariableSymbol Root, List<string> Path, string Name);
    private readonly Stack<List<Borrow>> borrows = new();


    public IReadOnlyList<Diagnostic> Diagnostics => diagnostics;

    public IReadOnlyDictionary<Expression, Type> Types => types;

    // What each name, and each error literal, resolved to.
    public IReadOnlyDictionary<Expression, Symbol> Symbols => symbols;
    public IReadOnlyDictionary<FunctionDeclaration, FunctionSymbol> Functions => functions;
    public IReadOnlyDictionary<StructDeclaration, StructType> Structs => structs;

    // Every declared error, in the order of their numbers.
    public IReadOnlyList<ErrorSymbol> Errors => errors;

    // The variable each parameter or declaration introduced: where the code generator finds its slot.
    public IReadOnlyDictionary<TypedName, VariableSymbol> Variables => variables;

    // The variable each catch block binds its failure to.
    public IReadOnlyDictionary<CatchBlock, VariableSymbol> CatchVariables => catchVariables;

    // The variables the pod keeps between calls, in the order they are laid out in the instance.
    public IReadOnlyList<VariableSymbol> States => states;


    public TypeChecker(ProgramNode program)
    {
        this.program = program;
        scope = global;
    }


    public void Check()
    {
        CollectStructs();
        CheckStructCycles();
        CollectErrors();
        CollectStates();
        CollectFunctions();

        foreach (var declaration in program.Functions) {
            CheckFunction(declaration);
        }
    }


    // ---- Pass 1: declarations ----

    // Every struct is declared with no fields first, then the fields are resolved, so that
    // "Point start" works even when Point is declared further down.
    private void CollectStructs()
    {
        foreach (var declaration in program.Structs) {
            var type = new StructType(declaration.Name.value, []);
            structs[declaration] = type;

            if (!global.Declare(new TypeSymbol(type.Name, type))) {
                Error(declaration.Name, $"'{type.Name}' is already declared");
            }
        }

        foreach (var declaration in program.Structs) {
            var type = structs[declaration];

            foreach (var field in declaration.Fields) {
                if (type.FindField(field.Name.value) != null) {
                    Error(field.Name, $"struct '{type.Name}' already has a field '{field.Name.value}'");
                    continue;
                }

                type.Fields.Add(new StructField(field.Name.value, ResolveType(field.Type)));
            }
        }
    }


    // A struct holding itself by value would be infinite; through an own it is a tree. A field
    // that closes a cycle is made invalid, so that sizes can be computed from there on.
    private void CheckStructCycles()
    {
        foreach (var declaration in program.Structs) {
            var type = structs[declaration];

            for (var i = 0; i < type.Fields.Count; i++) {
                if (type.Fields[i].Type is StructType inner && Contains(inner, type, [])) {
                    Error(declaration.Fields[i].Name, $"struct '{type.Name}' contains itself: own the field instead");
                    type.Fields[i] = type.Fields[i] with { Type = Type.Invalid };
                }
            }
        }
    }


    private static bool Contains(StructType type, StructType target, HashSet<StructType> seen)
    {
        if (type == target) return true;
        if (!seen.Add(type)) return false;

        return type.Fields.Any(f => f.Type is StructType inner && Contains(inner, target, seen));
    }


    // Errors are numbered from one in the order they are declared; zero means no error.
    private void CollectErrors()
    {
        foreach (var declaration in program.Errors) {
            var symbol = new ErrorSymbol(declaration.Name.value, errors.Count + 1);

            if (!errorsByName.TryAdd(symbol.Name, symbol)) {
                Error(declaration.Name, $"error '{symbol.Name}' is already declared");
                continue;
            }

            errors.Add(symbol);
        }
    }


    // State variables are global names, laid out in the instance in the order declared.
    private void CollectStates()
    {
        foreach (var declaration in program.States) {
            var variable = declaration.Variable;
            var symbol = new VariableSymbol(variable.Name.value, ResolveType(variable.Type), IsState: true);
            variables[variable] = symbol;
            states.Add(symbol);

            if (!global.Declare(symbol)) {
                Error(variable.Name, $"'{symbol.Name}' is already declared");
            }
        }
    }


    private void CollectFunctions()
    {
        foreach (var declaration in program.Functions) {
            var parameters = declaration.Parameters.Select(p => new Parameter(p.Name.value, ResolveType(p.Type), p.Ref != null)).ToList();
            var returnTypes = declaration.ReturnTypes.Select(ResolveType).ToList();
            var symbol = new FunctionSymbol(declaration.Name.value, parameters, returnTypes, declaration.Fails != null, declaration.External != null);
            functions[declaration] = symbol;

            if (!global.Declare(symbol)) {
                Error(declaration.Name, $"'{symbol.Name}' is already declared");
            }
        }
    }


    // The type as written: "int", "Point", "own Node", "own Node?". Only a struct can be owned.
    private Type ResolveType(TypeName type)
    {
        var name = type.Name;
        Type resolved;

        switch (global.Lookup(name.value)) {
            case TypeSymbol typeSymbol:
                resolved = typeSymbol.Type;
                break;
            case null:
                Error(name, $"unknown type '{name.value}'");
                return Type.Invalid;
            default:
                Error(name, $"'{name.value}' is not a type");
                return Type.Invalid;
        }

        if (type.Own == null) {
            if (type.Optional != null) Error(type.Optional, "only an own can be optional: write 'own T?'");
            return resolved;
        }

        if (resolved is not StructType structType) {
            Error(type.Own, $"only a struct can be owned, not {resolved}");
            return Type.Invalid;
        }

        return new OwnType(structType, type.Optional != null);
    }


    // ---- Pass 2: bodies ----

    private void CheckFunction(FunctionDeclaration declaration)
    {
        currentFunction = functions[declaration];
        scope = new Scope(global);
        moved.Clear();
        depth = 0;
        borrows.Push([]);

        // Nothing owning memory crosses to the host: it would carry an address.
        if (currentFunction.IsExternal) {
            var crossing = currentFunction.Parameters.Select(p => p.Type).Concat(currentFunction.ReturnTypes);
            if (crossing.Any(t => t.IsMovable)) {
                Error(declaration.Name, $"'{currentFunction.Name}' is external: it cannot take or return an own");
            }
        }

        for (var i = 0; i < declaration.Parameters.Count; i++) {
            var parameter = declaration.Parameters[i];
            var symbol = new VariableSymbol(parameter.Name.value, currentFunction.Parameters[i].Type, parameter.Ref != null);
            variables[parameter] = symbol;
            depths[symbol] = depth;

            if (!scope.Declare(symbol)) {
                Error(parameter.Name, $"duplicate parameter '{symbol.Name}'");
            }
        }

        foreach (var statement in declaration.Body) {
            CheckStatement(statement);
        }

        if (currentFunction.ReturnTypes.Count > 0 && !AlwaysReturns(declaration.Body)) {
            Error(declaration.Name, $"not all paths of '{declaration.Name.value}' return a value");
        }

        borrows.Pop();
        scope = global;
        currentFunction = null;
    }


    // Whether running the block always ends on a return: its last statement is one, or an
    // if with an else whose every branch always returns. A while is never counted on.
    private static bool AlwaysReturns(List<Statement> body)
    {
        if (body.Count == 0) return false;

        return body[^1] switch {
            ReturnStatement => true,
            IfStatement { Else: not null } ifStatement =>
                ifStatement.Arms.All(arm => AlwaysReturns(arm.Body)) && AlwaysReturns(ifStatement.Else),
            _ => false,
        };
    }


    private void CheckBlock(List<Statement> body)
    {
        EnterBlock();

        foreach (var statement in body) {
            CheckStatement(statement);
        }

        LeaveBlock();
    }


    private void EnterBlock()
    {
        scope = new Scope(scope);
        depth++;
        borrows.Push([]);
    }


    private void LeaveBlock()
    {
        borrows.Pop();
        depth--;
        scope = scope.Parent!;
    }


    private void CheckStatement(Statement statement)
    {
        switch (statement) {
            case VariableDeclaration declaration:
                CheckVariableDeclaration(declaration);
                break;

            case Assignment assignment:
                CheckAssignment(assignment);
                break;

            case CallStatement callStatement:
                CheckCallStatement(callStatement);
                break;

            case IfStatement ifStatement:
                CheckIf(ifStatement);
                break;

            case WhileStatement whileStatement:
                CheckWhile(whileStatement);
                break;

            case ReturnStatement returnStatement:
                CheckReturn(returnStatement);
                break;
        }
    }


    // What was moved in any branch is moved afterwards; a branch that always returns leads
    // nowhere, so what it moved does not count. Without an else, the path around counts.
    private void CheckIf(IfStatement ifStatement)
    {
        var before = new HashSet<VariableSymbol>(moved, ReferenceEqualityComparer.Instance);
        var after = new HashSet<VariableSymbol>(ReferenceEqualityComparer.Instance);

        foreach (var arm in ifStatement.Arms) {
            moved = new HashSet<VariableSymbol>(before, ReferenceEqualityComparer.Instance);
            if (arm.Binding != null) {
                CheckBoundArm(arm);
            } else {
                CheckCondition(arm.Condition);
                CheckBlock(arm.Body);
            }
            if (!AlwaysReturns(arm.Body)) after.UnionWith(moved);
        }

        if (ifStatement.Else != null) {
            moved = new HashSet<VariableSymbol>(before, ReferenceEqualityComparer.Instance);
            CheckBlock(ifStatement.Else);
            if (!AlwaysReturns(ifStatement.Else)) after.UnionWith(moved);
        } else {
            after.UnionWith(before);
        }

        moved = after;
    }


    // if ref Node c = ref tree.left: the arm runs with the name bound to what the own holds,
    // and only when it holds something.
    private void CheckBoundArm(IfArm arm)
    {
        EnterBlock();
        var type = CheckOptionalValue(arm.Binding!, arm.Condition);
        DeclareBinding(arm.Binding!, type, arm.Condition);

        foreach (var statement in arm.Body) {
            CheckStatement(statement);
        }

        LeaveBlock();
    }


    // ref Node c = ref tree.left or: the block runs when the own holds nothing and has to
    // leave the function, so that the name is bound for everything that follows.
    private void CheckOrDeclaration(VariableDeclaration declaration)
    {
        var target = declaration.Targets[0];
        if (declaration.Targets.Count > 1) {
            Error(declaration.Targets[1].Name, "a declaration with 'or:' binds one name");
        }

        var type = CheckOptionalValue(target, declaration.Value);

        var before = new HashSet<VariableSymbol>(moved, ReferenceEqualityComparer.Instance);
        CheckBlock(declaration.Otherwise!);
        moved = before;

        if (!AlwaysReturns(declaration.Otherwise!)) {
            Error(target.Name, "the 'or:' block must leave the function");
        }

        DeclareBinding(target, type, declaration.Value);
    }


    // The value an own that may be none is bound from: "ref tree.left" gives a ref to what it
    // holds, "take head" or a call gives the own itself, no longer optional. The declared type
    // is what the name gets.
    private Type CheckOptionalValue(TypedName target, Expression value)
    {
        var declared = ResolveType(target.Type);
        Type valueType;

        if (target.Ref != null) {
            if (value is not RefExpression reference) {
                Error(StartOf(value), "a ref variable must be bound with 'ref'");
                return declared;
            }
            valueType = CheckRef(reference, allowOptional: true);
        } else {
            valueType = CheckExpression(value);
            CheckMove(value);
        }

        if (valueType is not OwnType { Optional: true } optional) {
            if (valueType is not InvalidType) Error(StartOf(value), $"expected an own that may be none, got {valueType}");
            return declared;
        }

        Type bound = target.Ref != null ? optional.Inner : new OwnType(optional.Inner, false);
        if (!declared.Accepts(bound)) {
            Error(target.Name, $"cannot bind {bound} to '{target.Name.value}' of type {declared}");
        }

        return declared;
    }


    private void DeclareBinding(TypedName target, Type type, Expression value)
    {
        var symbol = new VariableSymbol(target.Name.value, type, target.Ref != null);
        variables[target] = symbol;
        depths[symbol] = depth;

        if (!scope.Declare(symbol)) {
            Error(target.Name, $"'{target.Name.value}' is already declared in this block");
        }

        if (target.Ref != null && value is RefExpression reference && PlaceOf(reference.Target) is { } place) {
            borrows.Peek().Add(new Borrow(place.Root, place.Path, symbol.Name));
        }
    }


    // A variable from around the loop that the body moves would be moved again next time
    // around, unless the body gives it a value again before its end.
    private void CheckWhile(WhileStatement whileStatement)
    {
        var before = new HashSet<VariableSymbol>(moved, ReferenceEqualityComparer.Instance);

        CheckCondition(whileStatement.Condition);
        CheckBlock(whileStatement.Body);

        foreach (var variable in moved.Where(v => !before.Contains(v) && depths[v] <= depth)) {
            Error(moveSites[variable], $"'{variable.Name}' is moved inside a loop");
        }
    }


    // int x = 1, and int s, int d = operate(x, y) where the call must yield exactly one
    // value per target. The value is checked before the names are declared, so that
    // "int x = x" reports x as undefined. A value that owns memory is moved into the name.
    private void CheckVariableDeclaration(VariableDeclaration declaration)
    {
        if (declaration.Otherwise != null) {
            CheckOrDeclaration(declaration);
            return;
        }

        var targetTypes = declaration.Targets.Select(t => ResolveType(t.Type)).ToList();
        var valueTypes = IsRefDeclaration(declaration)
            ? [CheckRef((RefExpression)declaration.Value)]
            : CheckValues(declaration.Value, targetTypes);

        if (declaration.Targets.Count == 1 && declaration.Targets[0].Ref == null) {
            CheckMove(declaration.Value);
        }

        if (valueTypes.Count != targetTypes.Count) {
            Error(StartOf(declaration.Value), $"expected {targetTypes.Count} value(s), got {valueTypes.Count}");
        } else {
            for (var i = 0; i < targetTypes.Count; i++) {
                if (!targetTypes[i].Accepts(valueTypes[i])) {
                    Error(declaration.Targets[i].Name, $"cannot assign {valueTypes[i]} to '{declaration.Targets[i].Name.value}' of type {targetTypes[i]}");
                }
            }
        }

        for (var i = 0; i < declaration.Targets.Count; i++) {
            var target = declaration.Targets[i];
            var symbol = new VariableSymbol(target.Name.value, targetTypes[i], target.Ref != null);
            variables[target] = symbol;
            depths[symbol] = depth;

            if (!scope.Declare(symbol)) {
                Error(target.Name, $"'{target.Name.value}' is already declared in this block");
            }

            if (target.Ref != null && declaration.Value is RefExpression reference && PlaceOf(reference.Target) is { } place) {
                borrows.Peek().Add(new Borrow(place.Root, place.Path, symbol.Name));
            }
        }
    }


    // ---- Moves and borrows ----

    // A value that owns memory is never copied: used as a value, a variable is moved and dies,
    // a field can not be (take a ref into it, or take it out with "take"), and anything else,
    // a call, a construction, none, take, is fresh and free to give.
    private void CheckMove(Expression value)
    {
        if (types.GetValueOrDefault(value) is not { IsMovable: true }) return;

        switch (value) {
            case NameExpression name when symbols.GetValueOrDefault(name) is VariableSymbol variable:
                if (variable.IsRef) {
                    Error(name.Name, $"cannot move out of ref '{variable.Name}'");
                } else if (variable.IsState) {
                    Error(name.Name, $"cannot move out of state '{variable.Name}': take a ref into it, or take it with 'take'");
                } else {
                    CheckNotBorrowed(name, name.Name, "move");
                    moved.Add(variable);
                    moveSites[variable] = name.Name;
                }
                break;

            case FieldExpression field:
                Error(StartOf(field), "cannot move out of a field: take a ref into it, or take it with 'take'");
                break;
        }
    }


    // The variable an expression is rooted in and the fields it goes through, for a name or a
    // chain of fields; nothing for a value.
    private (VariableSymbol Root, List<string> Path)? PlaceOf(Expression expression)
    {
        var path = new List<string>();

        while (expression is FieldExpression field) {
            path.Insert(0, field.Field.value);
            expression = field.Target;
        }

        if (expression is NameExpression name && symbols.GetValueOrDefault(name) is VariableSymbol root) {
            return (root, path);
        }

        return null;
    }


    private void CheckNotBorrowed(Expression place, Token at, string action)
    {
        if (PlaceOf(place) is not { } target) return;

        foreach (var borrow in borrows.SelectMany(list => list)) {
            if (borrow.Root != target.Root || !IsPrefix(borrow.Path, target.Path) && !IsPrefix(target.Path, borrow.Path)) continue;

            var described = string.Join(".", target.Path.Prepend(target.Root.Name));
            Error(at, $"cannot {action} '{described}' while '{borrow.Name}' refers into it");
            return;
        }
    }


    private static bool IsPrefix(List<string> prefix, List<string> path)
    {
        return prefix.Count <= path.Count && prefix.SequenceEqual(path.Take(prefix.Count));
    }


    // ref Point p = ref line.start: one name bound to one place, for good. Whether the
    // declaration is of that shape, after reporting the shapes it can not take.
    private bool IsRefDeclaration(VariableDeclaration declaration)
    {
        var target = declaration.Targets.FirstOrDefault(t => t.Ref != null);
        if (target == null) return false;

        if (declaration.Targets.Count > 1) {
            Error(target.Ref!, "a ref variable must be declared on its own");
            return false;
        }

        if (declaration.Value is not RefExpression) {
            Error(StartOf(declaration.Value), "a ref variable must be bound with 'ref'");
            return false;
        }

        return true;
    }


    // Assigning a whole variable gives it a value again, moved or not; a field of a moved
    // variable has no owner to belong to. The old value of the target is freed by the store.
    private void CheckAssignment(Assignment assignment)
    {
        var targetType = assignment.Target is NameExpression name ? CheckAssignedName(name) : CheckExpression(assignment.Target);
        var valueType = CheckExpression(assignment.Value, targetType);

        if (!targetType.Accepts(valueType)) {
            Error(StartOf(assignment.Value), $"cannot assign {valueType} to a target of type {targetType}");
        }

        CheckMove(assignment.Value);
        CheckNotBorrowed(assignment.Target, StartOf(assignment.Target), "assign");

        if (assignment.Target is NameExpression revived && symbols.GetValueOrDefault(revived) is VariableSymbol variable) {
            moved.Remove(variable);
        }
    }


    private Type CheckAssignedName(NameExpression name)
    {
        if (scope.Lookup(name.Name.value) is VariableSymbol variable) {
            symbols[name] = variable;
            types[name] = variable.Type;
            return variable.Type;
        }

        return CheckExpression(name);
    }


    // A call on its own line runs for its effect on its ref arguments. A value it would
    // return has nowhere to go, and dropping one silently is the kind of mistake a contract
    // can not afford, so such a call must be used in an expression instead.
    private void CheckCallStatement(CallStatement statement)
    {
        var returnTypes = CheckValues(statement.Call, []);

        if (returnTypes.Count > 0 && !returnTypes.Contains(Type.Invalid)) {
            Error(StartOf(statement.Call), $"the call returns {returnTypes.Count} value(s) that are not used");
        }
    }


    private void CheckCondition(Expression condition)
    {
        var type = CheckExpression(condition, Type.Bool);

        if (!Type.Bool.Accepts(type)) {
            Error(StartOf(condition), $"condition must be bool, got {type}");
        }
    }


    // Besides its values, a function marked "!" may return an error: "return error.Frozen",
    // or "return err" to pass on the one a catch block received. "return find(x)" passes on
    // every value of a call that gives back as many as this function does.
    private void CheckReturn(ReturnStatement returnStatement)
    {
        var expected = currentFunction!.ReturnTypes;

        if (returnStatement.Values.Count == 1 && expected.Count > 1 && returnStatement.Values[0] is CallExpression or TryExpression) {
            var passed = CheckValues(returnStatement.Values[0], expected);
            if (passed.Count != expected.Count) {
                Error(returnStatement.Keyword, $"'{currentFunction.Name}' returns {expected.Count} value(s), the call gives {passed.Count}");
                return;
            }
            for (var i = 0; i < expected.Count; i++) {
                if (!expected[i].Accepts(passed[i])) {
                    Error(StartOf(returnStatement.Values[0]), $"value {i + 1} of the call is {passed[i]}, {expected[i]} is expected");
                }
            }
            return;
        }

        if (returnStatement.Values.Count == 1) {
            var type = CheckExpression(returnStatement.Values[0], expected.Count == 1 ? expected[0] : null);

            if (type is ErrorType) {
                if (!currentFunction.CanFail) {
                    Error(returnStatement.Keyword, $"'{currentFunction.Name}' cannot fail: mark it with '!' or handle the case");
                }
                return;
            }

            if (expected.Count == 1) {
                if (!expected[0].Accepts(type)) {
                    Error(StartOf(returnStatement.Values[0]), $"cannot return {type} where {expected[0]} is expected");
                }
                CheckMove(returnStatement.Values[0]);
                return;
            }
        }

        if (returnStatement.Values.Count != expected.Count) {
            Error(returnStatement.Keyword, $"'{currentFunction.Name}' returns {expected.Count} value(s), got {returnStatement.Values.Count}");
            foreach (var value in returnStatement.Values) CheckExpression(value);
            return;
        }

        for (var i = 0; i < expected.Count; i++) {
            var type = CheckExpression(returnStatement.Values[i], expected[i]);
            if (!expected[i].Accepts(type)) {
                Error(StartOf(returnStatement.Values[i]), $"cannot return {type} where {expected[i]} is expected");
            }
            CheckMove(returnStatement.Values[i]);
        }
    }


    // ---- Expressions ----

    // The types an expression yields: one, except for a call to a function that returns several,
    // bare or under a try or a catch block.
    private List<Type> CheckValues(Expression expression, List<Type> expected)
    {
        switch (expression) {
            case CallExpression call:
                var returnTypes = CheckCall(call);
                RequireHandled(call);
                return returnTypes;

            case TryExpression tryExpression:
                return CheckTry(tryExpression);

            case CatchBlock catchBlock:
                return CheckCatchBlock(catchBlock);

            default:
                return [CheckExpression(expression, expected.Count == 1 ? expected[0] : null)];
        }
    }


    // "expected" is what the context would like to see, and is only used to give integer
    // literals their type: "uint x = 5" makes the 5 a uint. Nothing else is inferred from it.
    private Type CheckExpression(Expression expression, Type? expected = null)
    {
        var type = ComputeType(expression, expected);
        types[expression] = type;
        return type;
    }


    private Type ComputeType(Expression expression, Type? expected)
    {
        switch (expression) {
            case IntegerLiteral literal:
                return CheckIntegerLiteral(literal, expected);

            case BoolLiteral:
                return Type.Bool;

            case NameExpression name:
                return CheckName(name);

            case UnaryExpression unary:
                return CheckUnary(unary, expected);

            case BinaryExpression binary:
                return CheckBinary(binary, expected);

            case CallExpression call:
                var returnTypes = CheckCall(call);
                RequireHandled(call);
                return SingleValue(returnTypes, call.LeftParen);

            case FieldExpression field:
                return CheckField(field);

            case RefExpression reference:
                Error(reference.Keyword, "'ref' is only allowed on a call argument or in a ref declaration");
                return CheckExpression(reference.Target);

            case ErrorLiteral literal:
                return CheckErrorLiteral(literal);

            case NewExpression newExpression:
                return CheckNew(newExpression);

            case NoneLiteral:
                return Type.None;

            case TakeExpression take:
                return CheckTake(take);

            case TryExpression tryExpression:
                return SingleValue(CheckTry(tryExpression), tryExpression.Call.LeftParen);

            case CatchExpression catchExpression:
                return CheckCatchValue(catchExpression);

            case CatchBlock catchBlock:
                return SingleValue(CheckCatchBlock(catchBlock), catchBlock.Call.LeftParen);

            default:
                return Type.Invalid;
        }
    }


    private Type CheckIntegerLiteral(IntegerLiteral literal, Type? expected)
    {
        var type = expected is UintType ? Type.Uint : Type.Int;
        var max = type == Type.Uint ? ulong.MaxValue : long.MaxValue;

        if (!ulong.TryParse(literal.Value.value, out var value) || value > max) {
            Error(literal.Value, $"integer literal is too large for {type}");
        }

        return type;
    }


    private Type CheckName(NameExpression name)
    {
        var symbol = scope.Lookup(name.Name.value);

        switch (symbol) {
            case VariableSymbol variable:
                symbols[name] = variable;
                if (moved.Contains(variable)) Error(name.Name, $"'{variable.Name}' was moved");
                return variable.Type;
            case null:
                Error(name.Name, $"undefined name '{name.Name.value}'");
                return Type.Invalid;
            case FunctionSymbol:
                Error(name.Name, $"'{name.Name.value}' is a function, not a value");
                return Type.Invalid;
            default:
                Error(name.Name, $"'{name.Name.value}' is a type, not a value");
                return Type.Invalid;
        }
    }


    private Type CheckUnary(UnaryExpression unary, Type? expected)
    {
        if (unary.Operator.type == TokenType.Not) {
            var operand = CheckExpression(unary.Operand, Type.Bool);
            if (!Type.Bool.Accepts(operand)) {
                Error(unary.Operator, $"'not' needs a bool, got {operand}");
            }
            return Type.Bool;
        }

        // Negating a uint makes no sense, so "-" is only ever an int operation.
        var type = CheckExpression(unary.Operand, Type.Int);
        if (!Type.Int.Accepts(type)) {
            Error(unary.Operator, $"unary '-' needs an int, got {type}");
            return Type.Invalid;
        }
        return Type.Int;
    }


    private Type CheckBinary(BinaryExpression binary, Type? expected)
    {
        var op = binary.Operator;
        var isArithmetic = op.type is TokenType.Plus or TokenType.Minus or TokenType.Star or TokenType.Slash or TokenType.Percent;
        var isLogical = op.type is TokenType.And or TokenType.Or;

        // For arithmetic the operands are of the result type, so the context's wish applies to
        // them too. A comparison's operands have nothing to do with its bool result.
        var operandHint = isArithmetic ? expected : isLogical ? Type.Bool : null;

        // A literal takes the type of the other side, whichever side it is on: "2 * x" with x uint.
        Type left, right;
        if (binary.Left is IntegerLiteral && binary.Right is not IntegerLiteral) {
            right = CheckExpression(binary.Right, operandHint);
            left = CheckExpression(binary.Left, right);
        } else {
            left = CheckExpression(binary.Left, operandHint);
            right = CheckExpression(binary.Right, left);
        }

        if (left is InvalidType || right is InvalidType) {
            return isArithmetic ? Type.Invalid : Type.Bool;
        }

        if (left != right) {
            Error(op, $"operator '{op.value}' cannot mix {left} and {right}");
            return isArithmetic ? Type.Invalid : Type.Bool;
        }

        if (isLogical) {
            if (left != Type.Bool) Error(op, $"operator '{op.value}' needs bools, got {left}");
            return Type.Bool;
        }

        if (op.type is TokenType.Equal or TokenType.NotEqual) {
            if (left is StructType) Error(op, $"structs cannot be compared with '{op.value}'");
            if (left is OwnType or NoneType) Error(op, "an own cannot be compared: bind it with 'if ref' to see whether it holds something");
            return Type.Bool;
        }

        if (left is ErrorType) {
            Error(op, $"an error can only be compared with '==' or '!='");
            return isArithmetic ? Type.Invalid : Type.Bool;
        }

        // Arithmetic and ordering both want numbers.
        if (!left.IsNumeric) {
            Error(op, $"operator '{op.value}' needs numbers, got {left}");
            return isArithmetic ? Type.Invalid : Type.Bool;
        }

        return isArithmetic ? left : Type.Bool;
    }


    // A call used as a value must produce exactly one.
    private Type SingleValue(List<Type> returnTypes, Token at)
    {
        if (returnTypes.Count == 1) return returnTypes[0];

        if (returnTypes.Count > 1) {
            Error(at, $"the call returns {returnTypes.Count} values, only one can be used here");
        } else if (!returnTypes.Contains(Type.Invalid)) {
            Error(at, "the call returns nothing");
        }

        return Type.Invalid;
    }


    // ---- Failures ----

    private Type CheckErrorLiteral(ErrorLiteral literal)
    {
        if (!errorsByName.TryGetValue(literal.Name.value, out var symbol)) {
            Error(literal.Name, $"unknown error '{literal.Name.value}'");
            return Type.Invalid;
        }

        symbols[literal] = symbol;
        return Type.Error;
    }


    // Whether the call is to a function that can fail, and so has to be handled.
    private bool Fallible(CallExpression call)
    {
        return symbols.GetValueOrDefault(call.Callee) is FunctionSymbol { CanFail: true };
    }


    // A call that can fail is never bare: its failure goes up with "try" or is dealt with by "catch".
    private void RequireHandled(CallExpression call)
    {
        if (!Fallible(call)) return;

        var options = currentFunction!.CanFail ? "'try' or 'catch'" : "'catch'";
        Error(StartOf(call), $"the call can fail: use {options}");
    }


    private List<Type> CheckTry(TryExpression tryExpression)
    {
        if (!currentFunction!.CanFail) {
            Error(tryExpression.Keyword, "'try' needs a function marked '!': settle the failure here with 'catch'");
        }

        var returnTypes = CheckCall(tryExpression.Call);

        if (!Fallible(tryExpression.Call)) {
            Error(tryExpression.Keyword, "the call cannot fail");
        }

        return returnTypes;
    }


    // f(x) catch 0: the fallback stands in for the one value the call would have given.
    private Type CheckCatchValue(CatchExpression catchExpression)
    {
        var returnTypes = CheckCall(catchExpression.Call);

        if (!Fallible(catchExpression.Call)) {
            Error(catchExpression.Keyword, "the call cannot fail");
        }

        if (returnTypes.Count != 1) {
            if (!returnTypes.Contains(Type.Invalid)) {
                Error(catchExpression.Keyword, $"'catch' with a value needs a call that returns one value, this one returns {returnTypes.Count}");
            }
            CheckExpression(catchExpression.Fallback);
            return Type.Invalid;
        }

        var fallback = CheckExpression(catchExpression.Fallback, returnTypes[0]);
        if (!returnTypes[0].Accepts(fallback)) {
            Error(StartOf(catchExpression.Fallback), $"the fallback should be {returnTypes[0]}, got {fallback}");
        }

        return returnTypes[0];
    }


    // f(x) catch err: with a block. The block sees the failure as a variable of type error and
    // must leave the function, so that whatever follows only runs with the call's values in hand.
    private List<Type> CheckCatchBlock(CatchBlock catchBlock)
    {
        var returnTypes = CheckCall(catchBlock.Call);

        if (!Fallible(catchBlock.Call)) {
            Error(catchBlock.Keyword, "the call cannot fail");
        }

        var before = new HashSet<VariableSymbol>(moved, ReferenceEqualityComparer.Instance);
        EnterBlock();

        var variable = new VariableSymbol(catchBlock.Error.value, Type.Error);
        catchVariables[catchBlock] = variable;
        depths[variable] = depth;
        if (!scope.Declare(variable)) {
            Error(catchBlock.Error, $"'{variable.Name}' is already declared in this block");
        }

        foreach (var statement in catchBlock.Body) {
            CheckStatement(statement);
        }

        LeaveBlock();
        moved = before;

        if (!AlwaysReturns(catchBlock.Body)) {
            Error(catchBlock.Keyword, "the catch block must leave the function");
        }

        return returnTypes;
    }


    // abs(dx) calls a function, uint(x) converts, Point(1, 2) builds a struct: the name decides
    // which. A function call yields its return types, a cast or a construction a single value,
    // and an error the single Invalid type.
    private List<Type> CheckCall(CallExpression call)
    {
        if (call.Callee is not NameExpression name) {
            Error(StartOf(call.Callee), "only a function or a type can be called");
            CheckArguments(call, null);
            return [Type.Invalid];
        }

        var symbol = scope.Lookup(name.Name.value);

        switch (symbol) {
            case FunctionSymbol function:
                symbols[name] = function;
                CheckArguments(call, function.Parameters);
                return function.ReturnTypes;

            case TypeSymbol { Type: StructType structType } typeSymbol:
                symbols[name] = typeSymbol;
                return [CheckConstruction(call, structType)];

            case TypeSymbol typeSymbol:
                symbols[name] = typeSymbol;
                return [CheckCast(call, typeSymbol.Type)];

            case null:
                Error(name.Name, $"undefined function '{name.Name.value}'");
                CheckArguments(call, null);
                return [Type.Invalid];

            default:
                Error(name.Name, $"'{name.Name.value}' is not a function");
                CheckArguments(call, null);
                return [Type.Invalid];
        }
    }


    // Arguments are checked even when the callee is unknown, so their own errors still show.
    // A ref parameter takes a "ref" argument and nothing else, so that a call shows which of
    // the caller's variables it may change.
    private void CheckArguments(CallExpression call, List<Parameter>? parameters)
    {
        if (parameters != null && parameters.Count != call.Arguments.Count) {
            Error(call.LeftParen, $"expected {parameters.Count} argument(s), got {call.Arguments.Count}");
            parameters = null;
        }

        for (var i = 0; i < call.Arguments.Count; i++) {
            var argument = call.Arguments[i];
            var parameter = parameters?[i];
            Type type;

            if (argument is RefExpression reference) {
                type = CheckRef(reference);
                CheckNotBorrowed(reference.Target, reference.Keyword, "pass by ref");
                if (parameter is { IsRef: false }) {
                    Error(reference.Keyword, $"argument {i + 1} is not a ref parameter");
                }
            } else {
                type = CheckExpression(argument, parameter?.Type);
                CheckMove(argument);
                if (parameter is { IsRef: true }) {
                    Error(StartOf(argument), $"argument {i + 1} must be passed with 'ref'");
                }
            }

            if (parameter != null && !parameter.Type.Accepts(type)) {
                Error(StartOf(argument), $"argument {i + 1} should be {parameter.Type}, got {type}");
            }
        }
    }


    // What a ref names has to be somewhere to be written to: a variable, or a field of one.
    // A ref into an own is a ref to what it holds, so the own has to hold something.
    private Type CheckRef(RefExpression reference, bool allowOptional = false)
    {
        var type = CheckExpression(reference.Target);

        if (!reference.Target.IsAddressable) {
            Error(reference.Keyword, "'ref' needs a variable or a field, not a value");
        }

        if (type is OwnType own && !(allowOptional && own.Optional)) {
            type = own.Optional ? MayBeNone(reference.Keyword, own) : own.Inner;
        }

        types[reference] = type;
        return type;
    }


    private Type MayBeNone(Token at, OwnType own)
    {
        Error(at, $"{own} may be none: bind it with 'if ref' or 'or:' first");
        return Type.Invalid;
    }


    // new Node(1, none, none): built like Point(1, 2), but in memory of its own.
    private Type CheckNew(NewExpression newExpression)
    {
        if (global.Lookup(newExpression.Type.value) is not TypeSymbol { Type: StructType structType }) {
            Error(newExpression.Type, $"'{newExpression.Type.value}' is not a struct");
            foreach (var argument in newExpression.Arguments) CheckExpression(argument);
            return Type.Invalid;
        }

        CheckFields(newExpression.Arguments, structType, newExpression.Keyword);
        return new OwnType(structType, false);
    }


    // take head: only an optional own place can be left with none behind.
    private Type CheckTake(TakeExpression take)
    {
        var type = CheckExpression(take.Place);

        if (!take.Place.IsAddressable) {
            Error(take.Keyword, "'take' needs a variable or a field");
            return Type.Invalid;
        }

        if (type is not OwnType { Optional: true }) {
            if (type is not InvalidType) Error(take.Keyword, $"'take' needs an optional own, got {type}");
            return Type.Invalid;
        }

        CheckNotBorrowed(take.Place, take.Keyword, "take");
        return type;
    }


    // Only int <-> uint for now. The value is reinterpreted, never range checked: that is the
    // point of an explicit cast.
    private Type CheckCast(CallExpression call, Type target)
    {
        if (call.Arguments.Count != 1) {
            Error(call.LeftParen, $"a cast to {target} takes exactly one argument");
            foreach (var argument in call.Arguments) CheckExpression(argument);
            return Type.Invalid;
        }

        var source = CheckExpression(call.Arguments[0]);

        if (source is InvalidType) return target;

        if (!source.IsNumeric || !target.IsNumeric) {
            Error(call.LeftParen, $"cannot cast {source} to {target}");
            return Type.Invalid;
        }

        return target;
    }


    // Point(1, 2): one value per field, in the order of the declaration.
    private Type CheckConstruction(CallExpression call, StructType structType)
    {
        CheckFields(call.Arguments, structType, call.LeftParen);
        return structType;
    }


    private void CheckFields(List<Expression> values, StructType structType, Token at)
    {
        if (values.Count != structType.Fields.Count) {
            Error(at, $"'{structType.Name}' has {structType.Fields.Count} field(s), got {values.Count} value(s)");
            foreach (var value in values) CheckExpression(value);
            return;
        }

        for (var i = 0; i < values.Count; i++) {
            var field = structType.Fields[i];
            var type = CheckExpression(values[i], field.Type);
            if (!field.Type.Accepts(type)) {
                Error(StartOf(values[i]), $"field '{field.Name}' of '{structType.Name}' is {field.Type}, got {type}");
            }
            CheckMove(values[i]);
        }
    }


    // A field of an own is a field of what it holds, once it is known to hold something. A
    // value that owns memory has to be kept somewhere before its fields are read, or it would
    // be lost along with what it owns.
    private Type CheckField(FieldExpression field)
    {
        var target = CheckExpression(field.Target);

        if (target is InvalidType) return Type.Invalid;

        if (target.IsMovable && !field.Target.IsAddressable) {
            Error(field.Field, "a value that owns memory has to be kept in a variable before its fields are read");
            return Type.Invalid;
        }

        if (target is OwnType own) {
            target = own.Optional ? MayBeNone(field.Field, own) : own.Inner;
            if (target is InvalidType) return Type.Invalid;
        }

        if (target is not StructType structType) {
            Error(field.Field, $"{target} has no fields");
            return Type.Invalid;
        }

        var found = structType.FindField(field.Field.value);
        if (found == null) {
            Error(field.Field, $"struct '{structType.Name}' has no field '{field.Field.value}'");
            return Type.Invalid;
        }

        return found.Type;
    }


    // ---- Helpers ----

    // The token an expression starts with, to point an error at its beginning.
    private static Token StartOf(Expression expression)
    {
        return expression switch {
            IntegerLiteral literal => literal.Value,
            BoolLiteral literal => literal.Value,
            NameExpression name => name.Name,
            UnaryExpression unary => unary.Operator,
            BinaryExpression binary => StartOf(binary.Left),
            CallExpression call => StartOf(call.Callee),
            FieldExpression field => StartOf(field.Target),
            RefExpression reference => reference.Keyword,
            ErrorLiteral literal => literal.Keyword,
            NewExpression newExpression => newExpression.Keyword,
            NoneLiteral none => none.Keyword,
            TakeExpression take => take.Keyword,
            TryExpression tryExpression => tryExpression.Keyword,
            CatchExpression catchExpression => StartOf(catchExpression.Call),
            CatchBlock catchBlock => StartOf(catchBlock.Call),
            MissingExpression missing => missing.At,
            _ => throw new InvalidOperationException($"unknown expression {expression.GetType().Name}"),
        };
    }


    private void Error(Token at, string message)
    {
        diagnostics.Add(new Diagnostic(at.line, at.column, message));
    }
}
