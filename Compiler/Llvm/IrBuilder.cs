using System.Text;

namespace Compiler.Llvm;

// Writes the text of one function body. It knows how LLVM spells and numbers things and
// nothing about BlueCode: the emitters decide what to say, the builder decides how it is written.
public class IrBuilder
{
    // The allocas are collected apart so they can all sit at the top of the entry block,
    // which is what LLVM's promotion pass expects.
    private readonly StringBuilder allocas = new();
    private readonly StringBuilder body = new();
    private int nextRegister;
    private int nextLabel;
    private int nextSlot;


    // Nothing may follow a ret or br in the same block, and LLVM refuses it, so the emitters
    // check this before writing and stop until a new label opens a block.
    public bool Terminated { get; private set; }


    // Unnamed registers must be numbered in the order they appear, which emitting in order guarantees.
    public string Emit(string instruction)
    {
        var register = $"%{nextRegister++}";
        body.AppendLine($"  {register} = {instruction}");
        return register;
    }


    public void EmitVoid(string instruction)
    {
        body.AppendLine($"  {instruction}");
    }


    public void Terminate(string instruction)
    {
        EmitVoid(instruction);
        Terminated = true;
    }


    public void Label(string name)
    {
        body.AppendLine();
        body.AppendLine($"{name}:");
        Terminated = false;
    }


    // The labels of one statement share a number, so nested ifs and whiles never collide.
    public int NextLabelNumber()
    {
        return nextLabel++;
    }


    // A slot is named after its variable plus a counter, since two variables may share a name.
    public string Alloca(string name, string type)
    {
        var slot = $"%{name}.{nextSlot++}";
        allocas.AppendLine($"  {slot} = alloca {type}");
        return slot;
    }


    // A flag that is false from the entry on, whatever path leads to the code that reads it.
    public string Flag(string name)
    {
        var slot = Alloca(name, "i1");
        allocas.AppendLine($"  store i1 false, ptr {slot}");
        return slot;
    }


    // The allocas open the entry block, every other block follows in emission order.
    public override string ToString()
    {
        return new StringBuilder().AppendLine("entry:").Append(allocas).Append(body).ToString();
    }
}
