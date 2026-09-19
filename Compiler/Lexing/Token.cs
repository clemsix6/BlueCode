namespace Compiler.Lexing;

public enum TokenType
{
    Identifier,
    Integer,
    Struct,
    Def,
    External,
    State,
    If,
    Elif,
    Else,
    Return,
    True,
    False,
    While,
    And,
    Or,
    Not,
    Ref,
    Own,
    New,
    None,
    Take,
    Error,
    Try,
    Catch,
    Plus,
    Minus,
    Star,
    Slash,
    Percent,
    Bang,
    Question,
    Assign,
    Equal,
    NotEqual,
    Less,
    LessEqual,
    Greater,
    GreaterEqual,
    LeftParen,
    RightParen,
    Comma,
    Dot,
    Colon,
    Newline,
    Indent,
    Dedent,
    EndOfFile,
}


public class Token
{
    public readonly string value;
    public readonly TokenType type;
    public readonly int line;
    public readonly int column;


    public Token(string value, TokenType type, int line, int column)
    {
        this.value = value;
        this.type = type;
        this.line = line;
        this.column = column;
    }


    public override string ToString()
    {
        return $"{line}:{column}\t{type}\t{value}";
    }
}