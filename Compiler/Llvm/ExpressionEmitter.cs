using Compiler.Abi;
using Compiler.Lexing;
using Compiler.Semantics;
using Compiler.Syntax;
using Type = Compiler.Semantics.Type;

namespace Compiler.Llvm;

// What every expression of a function shares: the function, and the hidden state it carries.
// Depth is the register holding the depth left for callees. GasSlot holds the gas, LineSlot the
// line of the call in progress, for the trace should it give up, ResultSlot what the function
// returns, on its way to the exit block. Heap is the register pointing at the instance's heap.
// Flags tells, for each local that owns memory, where its held flag lives.
public record FunctionContext(FunctionSymbol Symbol, string Depth, string GasSlot, string LineSlot, string ResultSlot,
    string Heap, TraceEmitter Trace, DropEmitter Drops, IReadOnlyDictionary<VariableSymbol, string> Flags);


// Turns expressions into instructions. Every expression yields an operand, a register such
// as %3 or a constant such as 42, and the instructions that computed it are appended on the way.
public class ExpressionEmitter
{
    private readonly IrBuilder builder;
    private readonly TypeChecker checker;
    private readonly IReadOnlyDictionary<VariableSymbol, string> slots;
    private readonly FunctionContext context;

    // The held flags of locals read as values since the last transfer. A local stays the owner
    // until its value has actually gone somewhere, so that a failure on the way, a call that
    // gives up or a heap out of memory, still finds it in the exit block and frees it.
    private readonly List<string> pendingMoves = [];


    public ExpressionEmitter(IrBuilder builder, TypeChecker checker, IReadOnlyDictionary<VariableSymbol, string> slots, FunctionContext context)
    {
        this.builder = builder;
        this.checker = checker;
        this.slots = slots;
        this.context = context;
    }


    public string Emit(Expression expression)
    {
        switch (expression) {
            case IntegerLiteral literal:
                return literal.Value.value;

            case BoolLiteral literal:
                return literal.Value.value;

            case ErrorLiteral literal:
                return ((ErrorSymbol)checker.Symbols[literal]).Code.ToString();

            case NameExpression name:
                return EmitName(name);

            case NewExpression newExpression:
                return EmitNew(newExpression);

            case NoneLiteral:
                return "null";

            case TakeExpression take:
                return EmitTake(take);

            case UnaryExpression unary:
                return EmitUnary(unary);

            case BinaryExpression binary:
                return EmitBinary(binary);

            case CallExpression call:
                return EmitCallOrCast(call);

            case TryExpression tryExpression:
                return FirstValue(EmitTry(tryExpression));

            case CatchExpression catchExpression:
                return EmitCatch(catchExpression);

            case FieldExpression field:
                return EmitField(field);

            default:
                throw new InvalidOperationException($"cannot emit {expression.GetType().Name}");
        }
    }


    // Negating the smallest int overflows like any subtraction from zero.
    // Reading a local that owns memory as a value moves it, once the value is transferred.
    private string EmitName(NameExpression name)
    {
        var variable = (VariableSymbol)checker.Symbols[name];
        var value = builder.Emit($"load {IrType.Of(variable.Type)}, ptr {slots[variable]}");

        if (context.Flags.TryGetValue(variable, out var flag)) {
            pendingMoves.Add(flag);
        }

        return value;
    }


    // The values read since the last transfer have gone to their new owner: a callee, a fresh
    // block, a place, the caller. Their locals no longer hold them.
    public void Commit()
    {
        foreach (var flag in pendingMoves) {
            builder.EmitVoid($"store i1 false, ptr {flag}");
        }

        pendingMoves.Clear();
    }


    // new Node(1, none, none): the values first, so that a call among them that fails leaves no
    // block behind; then a block from the heap, or the out of memory fault; then the fields.
    private string EmitNew(NewExpression newExpression)
    {
        var structType = ((OwnType)TypeOf(newExpression)).Inner;
        var values = newExpression.Arguments.Select(Emit).ToList();

        var (sizeClass, bytes) = Heap.ClassOf(Layout.SizeOf(structType));
        var block = builder.Emit($"call ptr @bc_alloc(ptr {context.Heap}, i64 {sizeClass}, i64 {bytes})");
        BranchToFault(builder.Emit($"icmp eq ptr {block}, null"), "no_memory", newExpression.Keyword.line);

        for (var i = 0; i < values.Count; i++) {
            var field = builder.Emit($"getelementptr {IrType.Of(structType)}, ptr {block}, i32 0, i32 {i}");
            builder.EmitVoid($"store {IrType.Of(structType.Fields[i].Type)} {values[i]}, ptr {field}");
        }

        Commit();
        return block;
    }


    // take head: what the place holds, and none left in it.
    private string EmitTake(TakeExpression take)
    {
        var address = Address(take.Place);
        var value = builder.Emit($"load ptr, ptr {address}");
        builder.EmitVoid($"store ptr null, ptr {address}");
        return value;
    }


    private string EmitUnary(UnaryExpression unary)
    {
        var operand = Emit(unary.Operand);

        return unary.Operator.type == TokenType.Not
            ? builder.Emit($"xor i1 {operand}, true")
            : EmitChecked("ssub", "0", operand, unary.Operator.line);
    }


    // Arithmetic that would not fit, or a division by zero, is a fault: the function gives up
    // rather than compute something the source did not mean. Nothing here is left for LLVM to
    // treat as undefined.
    private string EmitBinary(BinaryExpression binary)
    {
        var left = Emit(binary.Left);
        var right = Emit(binary.Right);
        var operandType = TypeOf(binary.Left);
        var signed = operandType is IntType;
        var line = binary.Operator.line;

        switch (binary.Operator.type) {
            case TokenType.Plus:
                return EmitChecked(signed ? "sadd" : "uadd", left, right, line);
            case TokenType.Minus:
                return EmitChecked(signed ? "ssub" : "usub", left, right, line);
            case TokenType.Star:
                return EmitChecked(signed ? "smul" : "umul", left, right, line);
            case TokenType.Slash:
                return EmitDivision(signed ? "sdiv" : "udiv", left, right, signed, line);
            case TokenType.Percent:
                return EmitDivision(signed ? "srem" : "urem", left, right, signed, line);
        }

        var opcode = binary.Operator.type switch {
            TokenType.And => "and",
            TokenType.Or => "or",
            TokenType.Equal => "icmp eq",
            TokenType.NotEqual => "icmp ne",
            TokenType.Less => signed ? "icmp slt" : "icmp ult",
            TokenType.LessEqual => signed ? "icmp sle" : "icmp ule",
            TokenType.Greater => signed ? "icmp sgt" : "icmp ugt",
            TokenType.GreaterEqual => signed ? "icmp sge" : "icmp uge",
            _ => throw new InvalidOperationException($"unknown operator {binary.Operator.value}"),
        };

        return builder.Emit($"{opcode} {IrType.Of(operandType)} {left}, {right}");
    }


    // The intrinsics give the result and whether it overflowed, which the processor knows anyway.
    private string EmitChecked(string operation, string left, string right, int line)
    {
        var pair = builder.Emit($"call {{ i64, i1 }} @llvm.{operation}.with.overflow.i64(i64 {left}, i64 {right})");
        var value = builder.Emit($"extractvalue {{ i64, i1 }} {pair}, 0");
        var overflowed = builder.Emit($"extractvalue {{ i64, i1 }} {pair}, 1");
        BranchToFault(overflowed, "overflow", line);
        return value;
    }


    // Dividing by zero is a fault of its own; the one signed division that overflows, the
    // smallest int by -1, is an overflow like any other.
    private string EmitDivision(string opcode, string left, string right, bool signed, int line)
    {
        BranchToFault(builder.Emit($"icmp eq i64 {right}, 0"), "div_zero", line);

        if (signed) {
            var byMinusOne = builder.Emit($"icmp eq i64 {right}, -1");
            var ofSmallest = builder.Emit($"icmp eq i64 {left}, {long.MinValue}");
            BranchToFault(builder.Emit($"and i1 {byMinusOne}, {ofSmallest}"), "overflow", line);
        }

        return builder.Emit($"{opcode} i64 {left}, {right}");
    }


    // Jumps to one of the function's fault blocks, leaving it the line for the trace.
    private void BranchToFault(string condition, string fault, int line)
    {
        var next = $"safe{builder.NextLabelNumber()}";
        builder.EmitVoid($"store i64 {line}, ptr {context.LineSlot}");
        builder.Terminate($"br i1 {condition}, label %{fault}, label %{next}");
        builder.Label(next);
    }


    // uint(x) changes nothing at this level, both sides are i64: the argument is the result.
    // Point(1, 2) is the aggregate built one field at a time.
    private string EmitCallOrCast(CallExpression call)
    {
        var callee = (NameExpression)call.Callee;

        switch (checker.Symbols[callee]) {
            case TypeSymbol { Type: StructType structType }:
                var aggregate = "undef";
                for (var i = 0; i < call.Arguments.Count; i++) {
                    var value = Emit(call.Arguments[i]);
                    aggregate = builder.Emit($"insertvalue {IrType.Of(structType)} {aggregate}, {IrType.Of(structType.Fields[i].Type)} {value}, {i}");
                }
                return aggregate;

            case TypeSymbol:
                return Emit(call.Arguments[0]);

            default:
                return FirstValue(EmitCall(call));
        }
    }


    private string FirstValue((string Register, FunctionSymbol Function) result)
    {
        return builder.Emit($"extractvalue {IrType.Return(result.Function)} {result.Register}, 0");
    }


    // ---- Calls ----

    // Returns the aggregate the callee gave back, along with the callee, whose return type is
    // needed to unpack it. The gas the callee left is kept; if it gave up, so does this function,
    // after noting in the trace which line it was on.
    public (string Register, FunctionSymbol Function) EmitCall(CallExpression call)
    {
        var function = (FunctionSymbol)checker.Symbols[(NameExpression)call.Callee];
        var arguments = new List<string>();

        // A ref parameter receives the address of the caller's variable and works on it in place.
        for (var i = 0; i < call.Arguments.Count; i++) {
            var parameter = function.Parameters[i];
            arguments.Add(parameter.IsRef
                ? $"ptr {PlaceOf(((RefExpression)call.Arguments[i]).Target)}"
                : $"{IrType.Of(parameter.Type)} {Emit(call.Arguments[i])}");
        }

        arguments.Add($"i64 {builder.Emit($"load i64, ptr {context.GasSlot}")}");
        arguments.Add($"i64 {context.Depth}");
        arguments.Add("ptr %trace");
        arguments.Add("ptr %instance");

        builder.EmitVoid($"store i64 {call.LeftParen.line}, ptr {context.LineSlot}");
        var returnType = IrType.Return(function);
        var aggregate = builder.Emit($"call {returnType} @{function.Name}({string.Join(", ", arguments)})");
        Commit();

        var gas = builder.Emit($"extractvalue {returnType} {aggregate}, {IrType.GasIndex(function)}");
        builder.EmitVoid($"store i64 {gas}, ptr {context.GasSlot}");
        var failed = builder.Emit($"icmp slt i64 {gas}, 0");
        var next = $"call{builder.NextLabelNumber()}";
        builder.Terminate($"br i1 {failed}, label %abort, label %{next}");
        builder.Label(next);

        return (aggregate, function);
    }


    // The error the callee gave back, zero when it succeeded.
    public string ErrorOf((string Register, FunctionSymbol Function) result)
    {
        return builder.Emit($"extractvalue {IrType.Return(result.Function)} {result.Register}, {IrType.ErrorIndex(result.Function)}");
    }


    // try f(x): on failure this function returns the callee's error, one frame further.
    public (string Register, FunctionSymbol Function) EmitTry(TryExpression tryExpression)
    {
        var result = EmitCall(tryExpression.Call);
        var code = ErrorOf(result);
        var n = builder.NextLabelNumber();

        var failed = builder.Emit($"icmp ne i64 {code}, 0");
        builder.Terminate($"br i1 {failed}, label %fail{n}, label %ok{n}");

        builder.Label($"fail{n}");
        context.Trace.Append(tryExpression.Keyword.line.ToString(), "0");
        ReturnError(code);

        builder.Label($"ok{n}");
        return result;
    }


    // f(x) catch fallback: the value of the call, or the fallback's, met in one slot.
    private string EmitCatch(CatchExpression catchExpression)
    {
        var result = EmitCall(catchExpression.Call);
        var code = ErrorOf(result);
        var type = IrType.Of(result.Function.ReturnTypes[0]);
        var slot = builder.Alloca("catch", type);
        var n = builder.NextLabelNumber();

        var failed = builder.Emit($"icmp ne i64 {code}, 0");
        builder.Terminate($"br i1 {failed}, label %fallback{n}, label %ok{n}");

        builder.Label($"fallback{n}");
        builder.EmitVoid($"store {type} {Emit(catchExpression.Fallback)}, ptr {slot}");
        builder.Terminate($"br label %join{n}");

        builder.Label($"ok{n}");
        builder.EmitVoid($"store {type} {FirstValue(result)}, ptr {slot}");
        builder.Terminate($"br label %join{n}");

        builder.Label($"join{n}");
        return builder.Emit($"load {type}, ptr {slot}");
    }


    // Leaves the function with an error in place of its values, and the gas left.
    public void ReturnError(string code)
    {
        var function = context.Symbol;
        var returnType = IrType.Return(function);
        var gas = builder.Emit($"load i64, ptr {context.GasSlot}");
        var aggregate = builder.Emit($"insertvalue {returnType} zeroinitializer, i64 {code}, {IrType.ErrorIndex(function)}");
        Leave(builder.Emit($"insertvalue {returnType} {aggregate}, i64 {gas}, {IrType.GasIndex(function)}"));
    }


    // Puts the aggregate aside and heads for the exit block, which frees what is still owned
    // and returns it.
    public void Leave(string aggregate)
    {
        builder.EmitVoid($"store {IrType.Return(context.Symbol)} {aggregate}, ptr {context.ResultSlot}");
        Commit();
        builder.Terminate("br label %drops");
    }


    // ---- Fields and addresses ----

    // A field of something that has an address is loaded from that address; a field of a
    // value that has none, such as a call result, is picked out of the aggregate directly.
    private string EmitField(FieldExpression field)
    {
        if (field.Target.IsAddressable) {
            var address = Address(field);
            return builder.Emit($"load {IrType.Of(TypeOf(field))}, ptr {address}");
        }

        var target = Emit(field.Target);
        var structType = (StructType)TypeOf(field.Target);
        return builder.Emit($"extractvalue {IrType.Of(structType)} {target}, {FieldIndex(structType, field)}");
    }


    // Where the value of the expression is kept: for what can be assigned to or taken from. A
    // field of an own is inside the struct the own points to.
    public string Address(Expression expression)
    {
        switch (expression) {
            case NameExpression name:
                return slots[(VariableSymbol)checker.Symbols[name]];

            case FieldExpression field:
                var baseAddress = PlaceOf(field.Target);
                var structType = StructOf(TypeOf(field.Target));
                return builder.Emit($"getelementptr {IrType.Of(structType)}, ptr {baseAddress}, i32 0, i32 {FieldIndex(structType, field)}");

            default:
                throw new InvalidOperationException($"{expression.GetType().Name} has no address");
        }
    }


    // Where the struct the expression names lives: its address, or, for an own, the block it
    // points to. What a ref binds to.
    public string PlaceOf(Expression expression)
    {
        var address = Address(expression);
        return TypeOf(expression) is OwnType ? builder.Emit($"load ptr, ptr {address}") : address;
    }


    private static StructType StructOf(Type type)
    {
        return type is OwnType own ? own.Inner : (StructType)type;
    }


    private static int FieldIndex(StructType structType, FieldExpression field)
    {
        return structType.Fields.FindIndex(f => f.Name == field.Field.value);
    }


    private Type TypeOf(Expression expression)
    {
        return checker.Types[expression];
    }
}
