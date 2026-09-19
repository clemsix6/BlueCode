using Compiler.Lexing;

namespace Compiler.Syntax;

// A node that does something and has no value. A block is simply a List<Statement>.
public abstract record Statement;


// A type as written: int, Point, own Node, own Node?. Own marks a value kept in memory of its
// own and owned by whoever holds it; Optional, the "?", lets an own hold nothing.
public record TypeName(Token? Own, Token Name, Token? Optional);


// A type name paired with a name: a variable, a struct field or a parameter. Ref is the
// "ref" keyword when the name is bound to a place instead of holding a value of its own.
public record TypedName(TypeName Type, Token Name, Token? Ref = null);


// int x = 1, and the multi-declaration int s, int d = operate(x, y). With Otherwise, the value
// is an own that may be none: "ref Node c = ref tree.left or:" binds the name when there is
// something, and runs the block, which has to leave the function, when there is not.
public record VariableDeclaration(List<TypedName> Targets, Expression Value, List<Statement>? Otherwise = null) : Statement;


// x = 1, line.end.x = 1
public record Assignment(Expression Target, Expression Value) : Statement;


// deposit(ref account, 100): a call made for what it does to its ref arguments, not for a value.
// Also try deposit(...), and deposit(...) catch err: with its block.
public record CallStatement(Expression Call) : Statement;


// One "if" or "elif" arm: its condition and its block. With a Binding, the condition is an
// own that may be none, "if ref Node c = ref tree.left:", and the arm runs with the name bound
// when there is something.
public record IfArm(TypedName? Binding, Expression Condition, List<Statement> Body);


// The first arm is the "if", the following ones are the "elif"s, Else is absent without an "else".
public record IfStatement(Token Keyword, List<IfArm> Arms, List<Statement>? Else) : Statement;


// while x < 10: ...
public record WhileStatement(Token Keyword, Expression Condition, List<Statement> Body) : Statement;


// return, return x, return a, b
public record ReturnStatement(Token Keyword, List<Expression> Values) : Statement;
