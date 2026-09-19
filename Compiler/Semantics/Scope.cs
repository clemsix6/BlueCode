namespace Compiler.Semantics;

// The names visible at one point of the program. Lookup walks up through the parents, so a
// function body sees its parameters, then the global functions, structs and built-in types.
public class Scope
{
    private readonly Dictionary<string, Symbol> symbols = new();


    public Scope? Parent { get; }


    public Scope(Scope? parent = null)
    {
        Parent = parent;
    }


    // The outermost scope, with the built-in types already in it.
    public static Scope CreateGlobal()
    {
        var scope = new Scope();
        scope.Declare(new TypeSymbol("int", Type.Int));
        scope.Declare(new TypeSymbol("uint", Type.Uint));
        scope.Declare(new TypeSymbol("bool", Type.Bool));
        return scope;
    }


    // False when the name is already taken in this very scope. A name from an outer scope
    // is not a conflict here: whether shadowing is allowed is the checker's call.
    public bool Declare(Symbol symbol)
    {
        return symbols.TryAdd(symbol.Name, symbol);
    }


    public Symbol? Lookup(string name)
    {
        for (var scope = this; scope != null; scope = scope.Parent) {
            if (scope.symbols.TryGetValue(name, out var symbol)) return symbol;
        }

        return null;
    }


    // Only this scope, without the parents: what Declare checks against.
    public Symbol? LookupLocal(string name)
    {
        return symbols.GetValueOrDefault(name);
    }
}
