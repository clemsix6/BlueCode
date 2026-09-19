using Compiler.Abi;
using Compiler.Lexing;
using Compiler.Llvm;
using Compiler.Semantics;
using Compiler.Syntax;

class Program
{
    private static int Main(string[] args)
    {
        var path = args.FirstOrDefault(a => !a.StartsWith("--"));
        var showAst = args.Contains("--ast");
        var showManifest = args.Contains("--manifest");

        if (path == null) {
            Console.Error.WriteLine("usage: Compiler [--ast] [--manifest] <file.bc>");
            return 2;
        }

        var lexer = new Lexer(File.ReadAllText(path));
        var tokens = lexer.Lex();

        var parser = new Parser(tokens);
        var program = parser.ParseProgram();

        var checker = new TypeChecker(program);
        checker.Check();

        if (showAst) {
            Console.Error.Write(AstPrinter.Print(program));
        }

        var diagnostics = lexer.Diagnostics.Concat(parser.Diagnostics).Concat(checker.Diagnostics)
            .OrderBy(d => d.line).ThenBy(d => d.column).ToList();

        foreach (var diagnostic in diagnostics) {
            Console.Error.WriteLine($"{path}:{diagnostic}");
        }

        if (diagnostics.Count > 0) return 1;

        if (showManifest) {
            Console.WriteLine(Manifest.Build(program, checker, Path.GetFileName(path)).ToJson());
        } else {
            Console.Write(new ModuleEmitter(program, checker).Emit());
        }

        return 0;
    }
}
