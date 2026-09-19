using Compiler.Abi;
using Compiler.Semantics;
using Type = Compiler.Semantics.Type;

namespace Compiler.Llvm;

// How BlueCode types are spelled in LLVM. int and uint are both i64: the sign only shows in
// the instruction chosen for the operations where it matters, division, remainder and ordering.
// An error is its number, an i64 as well.
public static class IrType
{
    // A frame of the trace of a failure, and the trace itself: how many frames it holds, then
    // the frames. The host lays the same thing out in memory and reads it back.
    public const string Frame = "%Frame";
    public const string Trace = "%Trace";

    // What every call receives besides its arguments: the state of the instance, then its heap.
    public const string State = "%State";
    public const string Instance = "%Instance";


    public static string Of(Type type)
    {
        return type switch {
            IntType or UintType or ErrorType => "i64",
            BoolType => "i1",
            OwnType or NoneType => "ptr",
            StructType structType => $"%{structType.Name}",
            _ => throw new InvalidOperationException($"no LLVM type for {type}"),
        };
    }


    // A function returns its values, its error if it can fail, and the gas left, as one
    // aggregate: { results..., [i64 error,] i64 gas }. The values keep their indices; the
    // error is zero on success; a negative gas means the function gave up.
    public static string Return(FunctionSymbol function)
    {
        return Aggregate(function.ReturnTypes.Concat(HiddenReturns(function)));
    }


    public static int ErrorIndex(FunctionSymbol function)
    {
        return function.ReturnTypes.Count;
    }


    public static int GasIndex(FunctionSymbol function)
    {
        return function.ReturnTypes.Count + (function.CanFail ? 1 : 0);
    }


    // What the host sees as the results of a function: its values, then its error if it can fail.
    public static IEnumerable<Type> Results(FunctionSymbol function)
    {
        return function.CanFail ? function.ReturnTypes.Append(Type.Error) : function.ReturnTypes;
    }


    public static string Aggregate(IEnumerable<Type> types)
    {
        return $"{{ {string.Join(", ", types.Select(Of))} }}";
    }


    public static string TypeDefinitions(IEnumerable<VariableSymbol> state)
    {
        return $"{Frame} = type {{ i64, i64, i64 }}\n{Trace} = type {{ i64, [{Protocol.TraceCapacity} x {Frame}] }}\n"
             + $"{State} = type {Aggregate(state.Select(s => s.Type))}\n{Instance} = type {{ {State}, {Allocator.Heap} }}\n";
    }


    // The checked arithmetic every function may use. Intrinsics compile to instructions, so the
    // object stays free of anything to link.
    public static string IntrinsicDeclarations()
    {
        var operations = new[] { "sadd", "uadd", "ssub", "usub", "smul", "umul" };
        return string.Concat(operations.Select(o => $"declare {{ i64, i1 }} @llvm.{o}.with.overflow.i64(i64, i64)\n"));
    }


    private static IEnumerable<Type> HiddenReturns(FunctionSymbol function)
    {
        return function.CanFail ? [Type.Error, Type.Int] : [Type.Int];
    }
}
