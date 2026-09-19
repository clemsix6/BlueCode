using Compiler.Abi;

namespace Compiler.Llvm;

// Writes the trace of a failure: a frame per function it went through, from the one that
// raised it. Every function receives the trace as a hidden pointer and appends to it on its
// way out with an error or a fault, so the happy path never touches it. A frame holds the
// function's number, the line, and on the frame that raised the failure, its code; the frames
// that only passed it on carry zero there.
public class TraceEmitter
{
    private readonly IrBuilder builder;
    private readonly int function;


    public TraceEmitter(IrBuilder builder, int function)
    {
        this.builder = builder;
        this.function = function;
    }


    // The number of frames, kept at the head of the trace.
    public string Count()
    {
        return builder.Emit($"load i64, ptr %trace");
    }


    public void Restore(string count)
    {
        builder.EmitVoid($"store i64 {count}, ptr %trace");
    }


    // A failure raised outside any catch block starts a trace of its own.
    public void Reset()
    {
        Restore("0");
    }


    // Appends a frame, unless the trace is full: the count still grows, so the host knows.
    public void Append(string line, string code)
    {
        var n = builder.NextLabelNumber();
        var count = Count();
        var fits = builder.Emit($"icmp ult i64 {count}, {Protocol.TraceCapacity}");
        builder.Terminate($"br i1 {fits}, label %frame{n}, label %counted{n}");

        builder.Label($"frame{n}");
        var frame = builder.Emit($"getelementptr {IrType.Trace}, ptr %trace, i32 0, i32 1, i64 {count}");
        builder.EmitVoid($"store i64 {function}, ptr {frame}");
        builder.EmitVoid($"store i64 {line}, ptr {builder.Emit($"getelementptr {IrType.Frame}, ptr {frame}, i32 0, i32 1")}");
        builder.EmitVoid($"store i64 {code}, ptr {builder.Emit($"getelementptr {IrType.Frame}, ptr {frame}, i32 0, i32 2")}");
        builder.Terminate($"br label %counted{n}");

        builder.Label($"counted{n}");
        Restore(builder.Emit($"add i64 {count}, 1"));
    }
}
