using Compiler.Lexing;

namespace Compiler.Syntax;

// A node that has a value. Every concrete expression keeps the token it starts with,
// so that later stages can point error messages at the right place in the source.
public abstract record Expression
{
    // Whether the value lives somewhere: a variable, or a field of something that does.
    // Only such a place can be assigned to or passed by ref.
    public bool IsAddressable => this is NameExpression || this is FieldExpression access && access.Target.IsAddressable;
}


// 42
public record IntegerLiteral(Token Value) : Expression;


// true, false
public record BoolLiteral(Token Value) : Expression;


// x, line, abs (before it is called)
public record NameExpression(Token Name) : Expression;


// -x, not x
public record UnaryExpression(Token Operator, Expression Operand) : Expression;


// a + b, a < b, a and b. Precedence is already encoded in the shape of the tree,
// so every binary operator shares this one node.
public record BinaryExpression(Token Operator, Expression Left, Expression Right) : Expression;


// abs(dx), uint(-x). The callee is an expression, not a name, because the parser
// builds it with the same loop as field access.
public record CallExpression(Expression Callee, Token LeftParen, List<Expression> Arguments) : Expression;


// line.end, line.end.x (a FieldExpression whose target is another FieldExpression)
public record FieldExpression(Expression Target, Token Field) : Expression;


// ref account, ref line.start: the place the target names rather than its value, handed to
// a ref parameter or bound to a ref variable. It has no meaning anywhere else.
public record RefExpression(Token Keyword, Expression Target) : Expression;


// error.Frozen: one of the errors declared in the file.
public record ErrorLiteral(Token Keyword, Token Name) : Expression;


// new Node(1, none, none): a struct built in memory of its own, owned by whoever receives it.
public record NewExpression(Token Keyword, Token Type, List<Expression> Arguments) : Expression;


// none: nothing, for an own that may hold nothing.
public record NoneLiteral(Token Keyword) : Expression;


// take head: what an optional own place holds, moved out of it, leaving none behind.
public record TakeExpression(Token Keyword, Expression Place) : Expression;


// try withdraw(ref from, amount): the values of the call, or its failure handed up to the
// caller of the function this is in.
public record TryExpression(Token Keyword, CallExpression Call) : Expression;


// withdraw(ref account, amount) catch 0: the value of the call, or the fallback when it fails.
public record CatchExpression(CallExpression Call, Token Keyword, Expression Fallback) : Expression;


// withdraw(ref account, amount) catch err: with a block that looks at the failure under that
// name and has to leave the function. What follows the block only runs when the call succeeded.
public record CatchBlock(CallExpression Call, Token Keyword, Token Error, List<Statement> Body) : Expression;


// Stands where an expression was expected but none could be read. The error is already
// reported, this node only lets the parser keep going so it can find the next one.
public record MissingExpression(Token At) : Expression;
