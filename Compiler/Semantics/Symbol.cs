namespace Compiler.Semantics;

// What a name stands for once declared. The checker looks a name up, then switches on the
// kind of symbol it got back: a variable has a type, a function can be called, a type can
// declare variables or be used as a cast, as in "uint(x)".
public abstract record Symbol(string Name);


// IsRef: the name stands for a place that belongs to someone else, a ref parameter or a ref
// variable; nothing is ever moved out of it. IsState: the variable lives in the instance, from
// one call to the next; it is assigned, taken from or referred to, never moved.
public sealed record VariableSymbol(string Name, Type Type, bool IsRef = false, bool IsState = false) : Symbol(Name);


// CanFail is the "!" of the declaration: the function may give back an error instead of its values.
// IsExternal: the host may call it; only such functions get an entry and a place in the manifest.
public sealed record FunctionSymbol(string Name, List<Parameter> Parameters, List<Type> ReturnTypes, bool CanFail, bool IsExternal) : Symbol(Name);


// An error declared in the file, with the number the compiled code uses for it. Zero is never
// an error's number: it is what a function gives back when it succeeds.
public sealed record ErrorSymbol(string Name, int Code) : Symbol(Name);


// One parameter of a function. A ref parameter is the caller's variable itself rather than a
// copy of it: what the function writes to it stays written.
public sealed record Parameter(string Name, Type Type, bool IsRef);


// Both the built-in types and the declared structs: "int", "uint", "bool", "Point".
public sealed record TypeSymbol(string Name, Type Type) : Symbol(Name);
