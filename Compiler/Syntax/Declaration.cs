using Compiler.Lexing;

namespace Compiler.Syntax;

// A node that lives at file level.
public abstract record Declaration;


// struct Point: with its fields
public record StructDeclaration(Token Name, List<TypedName> Fields) : Declaration;


// error Frozen: a name a function can fail with.
public record ErrorDeclaration(Token Name) : Declaration;


// state own Node? root: a variable the pod keeps from one call to the next, in its instance.
public record StateDeclaration(TypedName Variable) : Declaration;


// def (int, int) operate (int x, int y): with its body. Fails is the "!" after the return
// types of a function that can fail: def uint! withdraw (...). External is the "external"
// before "def" of a function the host may call; the others are reachable from the pod only.
public record FunctionDeclaration(Token? External, List<TypeName> ReturnTypes, Token? Fails, Token Name, List<TypedName> Parameters, List<Statement> Body) : Declaration;


// The root of the tree: everything the parser found in one .bc file.
public record ProgramNode(List<StructDeclaration> Structs, List<ErrorDeclaration> Errors, List<StateDeclaration> States, List<FunctionDeclaration> Functions);
