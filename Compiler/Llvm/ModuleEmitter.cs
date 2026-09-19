using System.Text;
using Compiler.Semantics;
using Compiler.Syntax;
using Type = Compiler.Semantics.Type;

namespace Compiler.Llvm;

// Turns the checked tree into an LLVM module: the trace and struct types first, then one
// function after the other, each external one followed by the entry the host calls to reach it.
// Only those entries are visible from outside the module: an internal function has no symbol.
public class ModuleEmitter
{
    private readonly ProgramNode program;
    private readonly TypeChecker checker;


    public ModuleEmitter(ProgramNode program, TypeChecker checker)
    {
        this.program = program;
        this.checker = checker;
    }


    public string Emit()
    {
        var module = new StringBuilder();

        foreach (var declaration in program.Structs) {
            var type = checker.Structs[declaration];
            var fields = string.Join(", ", type.Fields.Select(f => IrType.Of(f.Type)));
            module.AppendLine($"%{type.Name} = type {{ {fields} }}");
        }

        if (program.Structs.Count > 0) module.AppendLine();

        module.AppendLine(IrType.TypeDefinitions(checker.States));
        module.AppendLine(IrType.IntrinsicDeclarations());
        module.AppendLine(Allocator.Emit());

        foreach (var declaration in program.Structs) {
            var type = checker.Structs[declaration];
            if (type.IsMovable) module.Append(EmitDrop(type)).AppendLine();
        }

        for (var i = 0; i < program.Functions.Count; i++) {
            var declaration = program.Functions[i];
            module.Append(new FunctionEmitter(checker, declaration, i).Emit());
            module.AppendLine();

            if (checker.Functions[declaration].IsExternal) {
                module.Append(new WrapperEmitter(checker, declaration).Emit());
                module.AppendLine();
            }
        }

        return module.ToString();
    }


    // Frees everything a struct owns, field by field; the struct itself belongs to its owner.
    private static string EmitDrop(StructType type)
    {
        var builder = new IrBuilder();
        var drops = new DropEmitter(builder, "%heap");

        for (var i = 0; i < type.Fields.Count; i++) {
            if (!type.Fields[i].Type.IsMovable) continue;
            var address = builder.Emit($"getelementptr %{type.Name}, ptr %value, i32 0, i32 {i}");
            drops.Drop(type.Fields[i].Type, address);
        }

        builder.Terminate("ret void");

        var text = new StringBuilder();
        text.AppendLine($"define internal void @drop.{type.Name}(ptr %heap, ptr %value) {{");
        text.Append(builder);
        text.AppendLine("}");
        return text.ToString();
    }
}
