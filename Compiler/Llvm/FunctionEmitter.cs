using System.Text;
using Compiler.Abi;
using Compiler.Lexing;
using Compiler.Semantics;
using Compiler.Syntax;

namespace Compiler.Llvm;

// Emits one function: its signature, then its statements. Locals live in alloca slots read
// with load and written with store; LLVM promotes them to registers itself, so no SSA
// bookkeeping is needed here. One instance per function, so its fields are that function's
// state and nothing has to be reset between two.
//
// Besides its parameters every function receives the gas left, the call depth allowed, the
// trace and the instance, and returns the gas along with its values. A function that gives up,
// because it went too deep or because a callee gave up, jumps to its abort block, which returns
// zero values and a negative gas naming the reason. The caller sees it and gives up in turn. A
// function that can fail returns its error the same way, as a value the caller has to look at.
//
// A local that owns memory has a flag telling whether it still holds its value: set when the
// value arrives, cleared when it is moved away or freed. Every way out of the function goes
// through one exit block that frees whatever is still flagged, so nothing owned is ever left
// behind, not even by a fault.
public class FunctionEmitter
{
    private readonly TypeChecker checker;
    private readonly FunctionDeclaration declaration;
    private readonly IrBuilder builder = new();
    private readonly TraceEmitter trace;
    private ExpressionEmitter expressions = null!;
    private FunctionContext context = null!;

    // Records compare by value: two "int y" in different blocks must still get their own slot.
    private readonly Dictionary<VariableSymbol, string> slots = new(ReferenceEqualityComparer.Instance);
    private readonly Dictionary<VariableSymbol, string> flags = new(ReferenceEqualityComparer.Instance);

    // The owning locals of each open block, innermost on top, freed when the block ends; and
    // all of them, for the exit block.
    private readonly Stack<List<VariableSymbol>> blockLocals = new();
    private readonly List<VariableSymbol> allLocals = [];

    // Where the gas lives inside the function, so that every return and the abort block find it,
    // and the line of the call in progress, for the trace should that call give up.
    private string gasSlot = "";
    private string lineSlot = "";

    // How long the trace was when each catch block began: what "return err" goes back to, so
    // that the failure goes on from where the call left it, whatever ran in the block since.
    private readonly Dictionary<VariableSymbol, string> catchCounts = new(ReferenceEqualityComparer.Instance);

    // The catch blocks being emitted, innermost on top: an error raised inside one is appended
    // to the chain of the failure it replaces.
    private readonly Stack<string> openCatches = new();


    // The function's number is its place in the file, which the manifest shares with the host.
    public FunctionEmitter(TypeChecker checker, FunctionDeclaration declaration, int number)
    {
        this.checker = checker;
        this.declaration = declaration;
        trace = new TraceEmitter(builder, number);
    }


    public string Emit()
    {
        var symbol = checker.Functions[declaration];
        var returnType = IrType.Return(symbol);
        blockLocals.Push([]);

        // A value parameter arrives in a register and is copied to a slot, so that it can be
        // assigned like any other variable. A ref parameter arrives as the address of the
        // caller's variable, which serves as the slot directly: every read and write goes there.
        var parameters = new List<string>();
        foreach (var parameter in declaration.Parameters) {
            var variable = checker.Variables[parameter];
            var type = IrType.Of(variable.Type);

            if (parameter.Ref != null) {
                slots[variable] = $"%{parameter.Name.value}";
                parameters.Add($"ptr nonnull dereferenceable({Layout.SizeOf(variable.Type)}) %{parameter.Name.value}");
                continue;
            }

            var slot = NewSlot(variable);
            parameters.Add($"{type} %{parameter.Name.value}");
            builder.EmitVoid($"store {type} %{parameter.Name.value}, ptr {slot}");
            if (flags.TryGetValue(variable, out var flag)) builder.EmitVoid($"store i1 true, ptr {flag}");
        }

        parameters.Add("i64 %gas");
        parameters.Add("i64 %depth");
        parameters.Add("ptr %trace");
        parameters.Add("ptr %instance");
        gasSlot = builder.Alloca("gas", "i64");
        lineSlot = builder.Alloca("line", "i64");
        var resultSlot = builder.Alloca("result", returnType);
        builder.EmitVoid($"store i64 %gas, ptr {gasSlot}");
        var heap = builder.Emit($"getelementptr {IrType.Instance}, ptr %instance, i32 0, i32 1");

        // The state variables live in the instance: their slots are inside it.
        for (var i = 0; i < checker.States.Count; i++) {
            slots[checker.States[i]] = builder.Emit($"getelementptr {IrType.State}, ptr %instance, i32 0, i32 {i}");
        }

        // One call deeper than the caller. Past the limit, nothing runs.
        var depth = builder.Emit("sub i64 %depth, 1");
        var tooDeep = builder.Emit($"icmp slt i64 {depth}, 0");
        builder.Terminate($"br i1 {tooDeep}, label %too_deep, label %body");

        builder.Label("body");
        context = new FunctionContext(symbol, depth, gasSlot, lineSlot, resultSlot, heap, trace, new DropEmitter(builder, heap), flags);
        expressions = new ExpressionEmitter(builder, checker, slots, context);
        EmitBlock(declaration.Body);

        // The checker made sure every path of a function with values returns, so the end can
        // only be reached through a block that all branches left with a ret.
        if (!builder.Terminated) {
            if (symbol.ReturnTypes.Count == 0) {
                EmitReturn(null, []);
            } else {
                builder.Terminate("unreachable");
            }
        }

        // A fault raised here starts the trace over with this function: too deep on arrival,
        // a division by zero, an overflow or a heap out of memory on the line kept in the slot.
        // A callee that gave up already wrote its frames; this function adds the line it
        // called from.
        EmitFault("too_deep", Protocol.AbortDepth, declaration.Name.line.ToString());
        EmitFault("div_zero", Protocol.AbortDivision, null);
        EmitFault("overflow", Protocol.AbortOverflow, null);
        EmitFault("no_memory", Protocol.AbortMemory, null);

        builder.Label("abort");
        trace.Append(builder.Emit($"load i64, ptr {lineSlot}"), "0");
        builder.Terminate("br label %fail");

        builder.Label("fail");
        var gas = builder.Emit($"load i64, ptr {gasSlot}");
        expressions.Leave(builder.Emit($"insertvalue {returnType} zeroinitializer, i64 {gas}, {IrType.GasIndex(symbol)}"));

        // The one way out: free what is still owned, then return what was put aside.
        builder.Label("drops");
        for (var i = allLocals.Count - 1; i >= 0; i--) {
            EmitDropLocal(allLocals[i]);
        }
        builder.Terminate($"ret {returnType} {builder.Emit($"load {returnType}, ptr {resultSlot}")}");

        // internal: reachable through its wrapper only, which also lets LLVM inline it there.
        var text = new StringBuilder();
        text.AppendLine($"define internal {returnType} @{symbol.Name}({string.Join(", ", parameters)}) {{");
        text.Append(builder);
        text.AppendLine("}");
        return text.ToString();
    }


    private void EmitFault(string label, int code, string? line)
    {
        builder.Label(label);
        trace.Reset();
        trace.Append(line ?? builder.Emit($"load i64, ptr {lineSlot}"), code.ToString());
        builder.EmitVoid($"store i64 {code}, ptr {gasSlot}");
        builder.Terminate("br label %fail");
    }


    // Nothing after a ret or br in the same block can run, so the rest of the block is simply
    // not emitted. A block that runs to its end frees the owning locals it declared.
    private void EmitBlock(List<Statement> statements, Action? prologue = null)
    {
        blockLocals.Push([]);
        prologue?.Invoke();

        foreach (var statement in statements) {
            if (builder.Terminated) break;
            EmitStatement(statement);
        }

        var locals = blockLocals.Pop();
        if (builder.Terminated) return;

        for (var i = locals.Count - 1; i >= 0; i--) {
            EmitDropLocal(locals[i]);
        }
    }


    // Frees the local's value if it still holds one, and notes that it no longer does.
    private void EmitDropLocal(VariableSymbol variable)
    {
        var flag = flags[variable];
        var n = builder.NextLabelNumber();

        var held = builder.Emit($"load i1, ptr {flag}");
        builder.Terminate($"br i1 {held}, label %drop{n}, label %kept{n}");

        builder.Label($"drop{n}");
        context.Drops.Drop(variable.Type, slots[variable]);
        builder.EmitVoid($"store i1 false, ptr {flag}");
        builder.Terminate($"br label %kept{n}");

        builder.Label($"kept{n}");
    }


    private void EmitStatement(Statement statement)
    {
        switch (statement) {
            case VariableDeclaration declaration:
                EmitVariableDeclaration(declaration);
                break;

            case Assignment assignment:
                EmitAssignment(assignment);
                break;

            case CallStatement callStatement:
                EmitHandled(callStatement.Call);
                break;

            case IfStatement ifStatement:
                EmitIf(ifStatement);
                break;

            case WhileStatement whileStatement:
                EmitWhile(whileStatement);
                break;

            case ReturnStatement returnStatement:
                EmitReturn(returnStatement.Keyword, returnStatement.Values);
                break;
        }
    }


    // The value is computed first, since it may read what the target held; then what the
    // target owned is freed, and the value stored.
    private void EmitAssignment(Assignment assignment)
    {
        var address = expressions.Address(assignment.Target);
        var value = assignment.Value is CatchBlock ? FirstValue(EmitHandled(assignment.Value)) : expressions.Emit(assignment.Value);
        var type = checker.Types[assignment.Target];

        if (assignment.Target is NameExpression name && flags.TryGetValue((VariableSymbol)checker.Symbols[name], out var flag)) {
            EmitDropLocal((VariableSymbol)checker.Symbols[name]);
            builder.EmitVoid($"store {IrType.Of(type)} {value}, ptr {address}");
            expressions.Commit();
            builder.EmitVoid($"store i1 true, ptr {flag}");
            return;
        }

        context.Drops.Drop(type, address);
        builder.EmitVoid($"store {IrType.Of(type)} {value}, ptr {address}");
        expressions.Commit();
    }


    private void EmitVariableDeclaration(VariableDeclaration declaration)
    {
        // ref Node c = ref tree.left or: the block runs, and leaves, when there is nothing.
        if (declaration.Otherwise != null) {
            var pointer = EmitOptional(declaration.Value);
            var n = builder.NextLabelNumber();
            var some = builder.Emit($"icmp ne ptr {pointer}, null");
            builder.Terminate($"br i1 {some}, label %bound{n}, label %none{n}");

            builder.Label($"none{n}");
            EmitBlock(declaration.Otherwise);
            if (!builder.Terminated) builder.Terminate("unreachable");

            builder.Label($"bound{n}");
            Bind(declaration.Targets[0], pointer);
            return;
        }

        // ref Point p = ref line.start: no slot of its own, the place it is bound to is the slot.
        if (declaration.Targets[0].Ref != null) {
            var target = ((RefExpression)declaration.Value).Target;
            slots[checker.Variables[declaration.Targets[0]]] = expressions.PlaceOf(target);
            return;
        }

        if (declaration.Targets.Count == 1 && declaration.Value is not CatchBlock) {
            var value = expressions.Emit(declaration.Value);
            StoreNew(declaration.Targets[0], value);
            return;
        }

        // int s, int d = operate(x, y): the call yields one aggregate, one field per target.
        var (aggregate, function) = EmitHandled(declaration.Value);
        var aggregateType = IrType.Return(function);

        for (var i = 0; i < declaration.Targets.Count; i++) {
            var value = builder.Emit($"extractvalue {aggregateType} {aggregate}, {i}");
            StoreNew(declaration.Targets[i], value);
        }
    }


    // What an own that may be none holds: the pointer in the place a ref names, or the own
    // value itself; null for none.
    private string EmitOptional(Expression value)
    {
        return value is RefExpression reference
            ? builder.Emit($"load ptr, ptr {expressions.Address(reference.Target)}")
            : expressions.Emit(value);
    }


    // A ref binding is the pointer itself, like a ref parameter; an own binding takes it over.
    private void Bind(TypedName target, string pointer)
    {
        if (target.Ref != null) {
            slots[checker.Variables[target]] = pointer;
        } else {
            StoreNew(target, pointer);
        }
    }


    // A call for all of its values: bare, under a try, or with a catch block.
    private (string Register, FunctionSymbol Function) EmitHandled(Expression call)
    {
        return call switch {
            CallExpression bare => expressions.EmitCall(bare),
            TryExpression tryExpression => expressions.EmitTry(tryExpression),
            CatchBlock catchBlock => EmitCatchBlock(catchBlock),
            _ => throw new InvalidOperationException($"{call.GetType().Name} is not a call"),
        };
    }


    private string FirstValue((string Register, FunctionSymbol Function) result)
    {
        return builder.Emit($"extractvalue {IrType.Return(result.Function)} {result.Register}, 0");
    }


    // f(x) catch err: the block runs with the error in a variable and has to leave the function,
    // so the code after it is only reached when the call succeeded.
    private (string Register, FunctionSymbol Function) EmitCatchBlock(CatchBlock catchBlock)
    {
        var result = expressions.EmitCall(catchBlock.Call);
        var code = expressions.ErrorOf(result);
        var n = builder.NextLabelNumber();

        var failed = builder.Emit($"icmp ne i64 {code}, 0");
        builder.Terminate($"br i1 {failed}, label %catch{n}, label %ok{n}");

        builder.Label($"catch{n}");
        var variable = checker.CatchVariables[catchBlock];
        builder.EmitVoid($"store i64 {code}, ptr {NewSlot(variable)}");
        var count = trace.Count();
        catchCounts[variable] = count;
        openCatches.Push(count);
        EmitBlock(catchBlock.Body);
        openCatches.Pop();
        if (!builder.Terminated) builder.Terminate("unreachable");

        builder.Label($"ok{n}");
        return result;
    }


    private void StoreNew(TypedName target, string value)
    {
        var variable = checker.Variables[target];
        var slot = NewSlot(variable);
        builder.EmitVoid($"store {IrType.Of(variable.Type)} {value}, ptr {slot}");
        expressions.Commit();
        if (flags.TryGetValue(variable, out var flag)) builder.EmitVoid($"store i1 true, ptr {flag}");
    }


    // A slot for the variable, and for one that owns memory, a flag and a place among the
    // locals its block and the exit block free.
    private string NewSlot(VariableSymbol variable)
    {
        var slot = builder.Alloca(variable.Name, IrType.Of(variable.Type));
        slots[variable] = slot;

        if (variable.Type.IsMovable && !variable.IsRef) {
            flags[variable] = builder.Flag(variable.Name + ".held");
            blockLocals.Peek().Add(variable);
            allLocals.Add(variable);
        }

        return slot;
    }


    // if c1: A elif c2: B else: C becomes
    //     br c1, then0.0, elif0.1
    //   then0.0: A; br endif0
    //   elif0.1: br c2, then0.1, else0
    //   then0.1: B; br endif0
    //   else0:   C; br endif0
    //   endif0:
    private void EmitIf(IfStatement ifStatement)
    {
        var n = builder.NextLabelNumber();
        var endLabel = $"endif{n}";

        for (var i = 0; i < ifStatement.Arms.Count; i++) {
            var arm = ifStatement.Arms[i];
            var isLast = i == ifStatement.Arms.Count - 1;
            var thenLabel = $"then{n}.{i}";
            var skipLabel = !isLast ? $"elif{n}.{i + 1}" : ifStatement.Else != null ? $"else{n}" : endLabel;

            if (arm.Binding != null) {
                var pointer = EmitOptional(arm.Condition);
                var some = builder.Emit($"icmp ne ptr {pointer}, null");
                builder.Terminate($"br i1 {some}, label %{thenLabel}, label %{skipLabel}");

                builder.Label(thenLabel);
                EmitBlock(arm.Body, () => Bind(arm.Binding, pointer));
            } else {
                var condition = expressions.Emit(arm.Condition);
                builder.Terminate($"br i1 {condition}, label %{thenLabel}, label %{skipLabel}");

                builder.Label(thenLabel);
                EmitBlock(arm.Body);
            }
            if (!builder.Terminated) builder.Terminate($"br label %{endLabel}");

            if (!isLast) builder.Label(skipLabel);
        }

        if (ifStatement.Else != null) {
            builder.Label($"else{n}");
            EmitBlock(ifStatement.Else);
            if (!builder.Terminated) builder.Terminate($"br label %{endLabel}");
        }

        builder.Label(endLabel);
    }


    private void EmitWhile(WhileStatement whileStatement)
    {
        var n = builder.NextLabelNumber();

        builder.Terminate($"br label %cond{n}");

        builder.Label($"cond{n}");
        var condition = expressions.Emit(whileStatement.Condition);
        builder.Terminate($"br i1 {condition}, label %body{n}, label %endwhile{n}");

        builder.Label($"body{n}");
        EmitBlock(whileStatement.Body);
        if (!builder.Terminated) builder.Terminate($"br label %cond{n}");

        builder.Label($"endwhile{n}");
    }


    // The values and the gas are returned as one aggregate, built field by field with insertvalue.
    // "return error.X" raises an error, "return err" passes on the one a catch block received;
    // either way the trace gets this line. The keyword is absent for the implicit return at the
    // end of a function without values.
    private void EmitReturn(Token? keyword, List<Expression> values)
    {
        var function = checker.Functions[declaration];

        if (function.CanFail && values.Count == 1 && checker.Types[values[0]] is ErrorType) {
            EmitErrorReturn(keyword!, values[0]);
            return;
        }

        var aggregateType = IrType.Return(function);
        var aggregate = "undef";

        // return find(x): the call's values, passed on one by one.
        if (values.Count == 1 && function.ReturnTypes.Count > 1) {
            var (called, callee) = EmitHandled(values[0]);
            for (var i = 0; i < function.ReturnTypes.Count; i++) {
                var value = builder.Emit($"extractvalue {IrType.Return(callee)} {called}, {i}");
                aggregate = builder.Emit($"insertvalue {aggregateType} {aggregate}, {IrType.Of(function.ReturnTypes[i])} {value}, {i}");
            }
            values = [];
        }

        for (var i = 0; i < values.Count; i++) {
            var value = expressions.Emit(values[i]);
            aggregate = builder.Emit($"insertvalue {aggregateType} {aggregate}, {IrType.Of(function.ReturnTypes[i])} {value}, {i}");
        }

        if (function.CanFail) {
            aggregate = builder.Emit($"insertvalue {aggregateType} {aggregate}, i64 0, {IrType.ErrorIndex(function)}");
        }

        var gas = builder.Emit($"load i64, ptr {gasSlot}");
        expressions.Leave(builder.Emit($"insertvalue {aggregateType} {aggregate}, i64 {gas}, {IrType.GasIndex(function)}"));
    }


    private void EmitErrorReturn(Token keyword, Expression value)
    {
        var code = expressions.Emit(value);

        if (value is ErrorLiteral) {
            // A new error starts a trace of its own, unless it replaces the failure a catch block
            // received: then it goes on from that failure's frames, as its cause.
            if (openCatches.Count > 0) trace.Restore(openCatches.Peek()); else trace.Reset();
            trace.Append(keyword.line.ToString(), code);
        } else {
            var variable = (VariableSymbol)checker.Symbols[(NameExpression)value];
            trace.Restore(catchCounts[variable]);
            trace.Append(keyword.line.ToString(), "0");
        }

        expressions.ReturnError(code);
    }
}
