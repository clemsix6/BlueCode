namespace Compiler.Lexing;

public class Lexer
{
    // One physical line: its text, the cursor of the line after it, and its leading space count.
    private readonly record struct Line(string Content, int Next, int Indentation);


    private static readonly Dictionary<string, TokenType> keywords = new() {
        ["struct"] = TokenType.Struct,
        ["def"] = TokenType.Def,
        ["external"] = TokenType.External,
        ["state"] = TokenType.State,
        ["if"] = TokenType.If,
        ["elif"] = TokenType.Elif,
        ["else"] = TokenType.Else,
        ["return"] = TokenType.Return,
        ["true"] = TokenType.True,
        ["false"] = TokenType.False,
        ["while"] = TokenType.While,
        ["and"] = TokenType.And,
        ["or"] = TokenType.Or,
        ["not"] = TokenType.Not,
        ["ref"] = TokenType.Ref,
        ["own"] = TokenType.Own,
        ["new"] = TokenType.New,
        ["none"] = TokenType.None,
        ["take"] = TokenType.Take,
        ["error"] = TokenType.Error,
        ["try"] = TokenType.Try,
        ["catch"] = TokenType.Catch,
    };


    // Tried before the one-character operators, so that "<=" is never read as "<" then "=".
    private static readonly Dictionary<string, TokenType> twoCharOperators = new() {
        ["=="] = TokenType.Equal,
        ["!="] = TokenType.NotEqual,
        ["<="] = TokenType.LessEqual,
        [">="] = TokenType.GreaterEqual,
    };


    private static readonly Dictionary<char, TokenType> oneCharOperators = new() {
        ['+'] = TokenType.Plus,
        ['-'] = TokenType.Minus,
        ['*'] = TokenType.Star,
        ['/'] = TokenType.Slash,
        ['%'] = TokenType.Percent,
        ['!'] = TokenType.Bang,
        ['?'] = TokenType.Question,
        ['='] = TokenType.Assign,
        ['<'] = TokenType.Less,
        ['>'] = TokenType.Greater,
        ['('] = TokenType.LeftParen,
        [')'] = TokenType.RightParen,
        [','] = TokenType.Comma,
        ['.'] = TokenType.Dot,
        [':'] = TokenType.Colon,
    };


    private readonly string fileContent;
    private readonly List<Token> tokens = [];
    private readonly List<Diagnostic> diagnostics = [];

    // Indentation width of every open block, innermost on top. The bottom entry is the top level.
    private readonly Stack<int> indents = new();

    // The line being scanned, its 1-based number, and the index of the next unread character in it.
    private Line line;
    private int lineNumber;
    private int pos;


    public IReadOnlyList<Diagnostic> Diagnostics => diagnostics;


    public Lexer(string fileContent)
    {
        this.fileContent = fileContent;
        indents.Push(0);
    }


    private Line GetLine(int cursor)
    {
        var end = fileContent.IndexOf('\n', cursor);
        if (end < 0) end = fileContent.Length;

        var content = fileContent[cursor..end].TrimEnd('\r');

        var indentation = 0;
        while (indentation < content.Length && content[indentation] == ' ') {
            indentation++;
        }

        return new Line(content, Math.Min(end + 1, fileContent.Length), indentation);
    }


    public List<Token> Lex()
    {
        for (var cursor = 0; cursor < fileContent.Length;) {
            line = GetLine(cursor);
            cursor = line.Next;
            lineNumber++;
            ScanLine();
        }

        // Close every block still open so the parser can rely on each Indent being matched.
        while (indents.Count > 1) {
            indents.Pop();
            tokens.Add(new Token("", TokenType.Dedent, lineNumber + 1, 1));
        }

        tokens.Add(new Token("", TokenType.EndOfFile, lineNumber + 1, 1));
        return tokens;
    }


    // A line holding only spaces or a comment produces nothing at all, not even a Newline,
    // so blank lines never disturb the indentation of the surrounding block.
    private void ScanLine()
    {
        pos = line.Indentation;
        if (IsBlankOrComment()) return;

        HandleIndentation(line.Indentation);

        while (pos < line.Content.Length) {
            var c = line.Content[pos];

            switch (c) {
                case ' ':
                    pos++;
                    continue;
                case '\t':
                    Error("tabs are not allowed, use spaces");
                    pos++;
                    continue;
            }

            if (c == '/' && Peek(1) == '/') break;

            ScanToken();
        }

        AddLayout(TokenType.Newline);
    }


    private bool IsBlankOrComment()
    {
        var i = pos;
        while (i < line.Content.Length && (line.Content[i] == ' ' || line.Content[i] == '\t')) {
            i++;
        }

        return i >= line.Content.Length || line.Content.AsSpan(i).StartsWith("//");
    }


    // Wider than the enclosing block opens a new one, narrower closes blocks until an equal
    // width is found, equal emits nothing.
    private void HandleIndentation(int width)
    {
        if (width > indents.Peek()) {
            indents.Push(width);
            AddLayout(TokenType.Indent);
            return;
        }

        while (width < indents.Peek()) {
            indents.Pop();
            AddLayout(TokenType.Dedent);
        }

        if (width != indents.Peek()) {
            Error("unindent does not match any outer indentation level");
        }
    }


    private void ScanToken()
    {
        var c = line.Content[pos];

        if (char.IsAsciiLetter(c) || c == '_') {
            ScanIdentifierOrKeyword();
        } else if (char.IsAsciiDigit(c)) {
            ScanInteger();
        } else {
            ScanOperator();
        }
    }


    private void ScanIdentifierOrKeyword()
    {
        var start = pos;
        while (pos < line.Content.Length && (char.IsAsciiLetterOrDigit(line.Content[pos]) || line.Content[pos] == '_')) {
            pos++;
        }

        var text = line.Content[start..pos];
        var type = keywords.TryGetValue(text, out var keyword) ? keyword : TokenType.Identifier;
        Add(type, text, start);
    }


    // Whether the value fits in 64 bits is checked later, with the types, not here.
    private void ScanInteger()
    {
        var start = pos;
        while (pos < line.Content.Length && char.IsAsciiDigit(line.Content[pos])) {
            pos++;
        }

        Add(TokenType.Integer, line.Content[start..pos], start);
    }


    // An unknown character is reported and skipped so that scanning continues and later errors are still found.
    private void ScanOperator()
    {
        var start = pos;

        if (pos + 1 < line.Content.Length && twoCharOperators.TryGetValue(line.Content.Substring(pos, 2), out var two)) {
            pos += 2;
            Add(two, line.Content[start..pos], start);
            return;
        }

        if (oneCharOperators.TryGetValue(line.Content[pos], out var one)) {
            pos++;
            Add(one, line.Content[start..pos], start);
            return;
        }

        Error($"unexpected character '{line.Content[pos]}'");
        pos++;
    }


    private char Peek(int offset)
    {
        var i = pos + offset;
        return i < line.Content.Length ? line.Content[i] : '\0';
    }


    private void Add(TokenType type, string value, int start)
    {
        tokens.Add(new Token(value, type, lineNumber, start + 1));
    }


    private void AddLayout(TokenType type)
    {
        tokens.Add(new Token("", type, lineNumber, pos + 1));
    }


    private void Error(string message)
    {
        diagnostics.Add(new Diagnostic(lineNumber, pos + 1, message));
    }
}
