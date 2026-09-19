using System.Text.Json;
using System.Text.Json.Serialization;
using Compiler.Llvm;
using Compiler.Semantics;
using Compiler.Syntax;
using Type = Compiler.Semantics.Type;

namespace Compiler.Abi;

// The contract between a compiled pod and its host: the depth limit the code enforces, the
// errors the code can fail with, every struct with the offsets of its fields, and every function.
// An external function comes with the symbol of its entry and the layout of its argument and
// result blocks; an internal one is listed by name only, for the traces. The host reads the
// manifest once when loading the pod, then only ever passes memory. An argument marked ref is
// one the function writes back into the block it was given; a function marked fails ends its
// results with its error. Functions are listed in the order of their numbers, which the frames
// of a trace refer to, and Source names the file for the lines of those frames. State is the
// block of the variables the pod keeps between calls, which the host lays out in the instance.
public sealed record Manifest(string Source, int MaxDepth, List<ErrorName> Errors, Dictionary<string, Block> Structs, Block State, List<Function> Functions)
{
    private static readonly JsonSerializerOptions jsonOptions = new() {
        WriteIndented = true,
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    };


    // Every function gets an entry the host can call, named after it.
    public static string SymbolOf(string function)
    {
        return $"bc_{function}";
    }


    public static Manifest Build(ProgramNode program, TypeChecker checker, string source)
    {
        var structs = new Dictionary<string, Block>();
        foreach (var declaration in program.Structs) {
            var type = checker.Structs[declaration];
            structs[type.Name] = Block.Of(type.Fields.Select(f => ((string?)f.Name, f.Type, false)));
        }

        var errors = checker.Errors.Select(e => new ErrorName(e.Name, e.Code)).ToList();

        var functions = new List<Function>();
        foreach (var declaration in program.Functions) {
            var symbol = checker.Functions[declaration];

            if (!symbol.IsExternal) {
                functions.Add(new Function(symbol.Name, null, null, null, null, null));
                continue;
            }

            var parameters = symbol.Parameters.Select(p => ((string?)p.Name, p.Type, p.IsRef));
            var results = IrType.Results(symbol).Select(t => ((string?)null, t, false));
            functions.Add(new Function(symbol.Name, true, SymbolOf(symbol.Name), symbol.CanFail ? true : null, Block.Of(parameters), Block.Of(results)));
        }

        var state = Block.Of(checker.States.Select(s => ((string?)s.Name, s.Type, false)));
        return new Manifest(source, Protocol.MaxDepth, errors, structs, state, functions);
    }


    public string ToJson()
    {
        return JsonSerializer.Serialize(this, jsonOptions);
    }
}


public sealed record ErrorName(string Name, int Code);


// External and Fails are only ever written when true; an internal function has only its name.
public sealed record Function(string Name, bool? External, string? Symbol, bool? Fails, Block? Args, Block? Results);


// Values laid out one after the other: a struct, or the arguments or results of a function.
// Results have no names.
public sealed record Block(int Size, List<Field> Fields)
{
    public static Block Of(IEnumerable<(string? Name, Type Type, bool IsRef)> members)
    {
        var list = members.ToList();
        var (offsets, size) = Layout.Place(list.Select(m => m.Type));
        var fields = list.Select((m, i) => new Field(m.Name, m.Type.ToString(), offsets[i], m.IsRef ? true : null)).ToList();
        return new Block(size, fields);
    }
}


// Ref is only ever written when true, on the arguments a function takes by ref.
public sealed record Field(string? Name, string Type, int Offset, bool? Ref);
