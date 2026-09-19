using System.Text;
using Compiler.Abi;
using Compiler.Semantics;
using Compiler.Syntax;

namespace Compiler.Llvm;

// Emits the entry the host calls for one function:
//     bc_f(ptr args, ptr results, ptr trace, ptr instance, i64 gas) -> i64
// The arguments sit in memory as one struct of the parameters, the results are written back as
// one struct of the return types, both laid out as the manifest says. Every function of every
// pod has this same shape, so the host only ever needs to know one signature. What comes back
// is the gas the function left, negative if it gave up; the results are then all zero.
// A ref parameter is worked on where it sits: after the call, the host reads what the function
// left in its args block. A function that can fail has its error as the last of its results,
// zero on success. The trace is where a failure or a fault leaves the frames it went through;
// the instance is the state the pod keeps between calls and the heap it allocates from.
public class WrapperEmitter
{
    private readonly TypeChecker checker;
    private readonly FunctionDeclaration declaration;
    private readonly IrBuilder builder = new();


    public WrapperEmitter(TypeChecker checker, FunctionDeclaration declaration)
    {
        this.checker = checker;
        this.declaration = declaration;
    }


    public string Emit()
    {
        var function = checker.Functions[declaration];
        var argsType = $"%{function.Name}.args";
        var resultsType = $"%{function.Name}.results";
        var text = new StringBuilder();

        if (function.Parameters.Count > 0) {
            text.AppendLine($"{argsType} = type {IrType.Aggregate(function.Parameters.Select(p => p.Type))}");
        }

        var results = IrType.Results(function).ToList();
        if (results.Count > 0) {
            text.AppendLine($"{resultsType} = type {IrType.Aggregate(results)}");
        }

        var arguments = new List<string>();
        for (var i = 0; i < function.Parameters.Count; i++) {
            var parameter = function.Parameters[i];
            var type = IrType.Of(parameter.Type);
            var address = builder.Emit($"getelementptr {argsType}, ptr %args, i32 0, i32 {i}");
            arguments.Add(parameter.IsRef ? $"ptr {address}" : $"{type} {builder.Emit($"load {type}, ptr {address}")}");
        }

        arguments.Add("i64 %gas");
        arguments.Add($"i64 {Protocol.MaxDepth}");
        arguments.Add("ptr %trace");
        arguments.Add("ptr %instance");

        var returnType = IrType.Return(function);
        var aggregate = builder.Emit($"call {returnType} @{function.Name}({string.Join(", ", arguments)})");

        for (var i = 0; i < results.Count; i++) {
            var type = IrType.Of(results[i]);
            var value = builder.Emit($"extractvalue {returnType} {aggregate}, {i}");
            var address = builder.Emit($"getelementptr {resultsType}, ptr %results, i32 0, i32 {i}");
            builder.EmitVoid($"store {type} {value}, ptr {address}");
        }

        var gas = builder.Emit($"extractvalue {returnType} {aggregate}, {IrType.GasIndex(function)}");
        builder.Terminate($"ret i64 {gas}");

        text.AppendLine($"define i64 @{Manifest.SymbolOf(function.Name)}(ptr %args, ptr %results, ptr %trace, ptr %instance, i64 %gas) {{");
        text.Append(builder);
        text.AppendLine("}");
        return text.ToString();
    }
}
