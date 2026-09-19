namespace Compiler;

public class Diagnostic
{
    public readonly int line;
    public readonly int column;
    public readonly string message;


    public Diagnostic(int line, int column, string message)
    {
        this.line = line;
        this.column = column;
        this.message = message;
    }


    public override string ToString()
    {
        return $"{line}:{column}: error: {message}";
    }
}
