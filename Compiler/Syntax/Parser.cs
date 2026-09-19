using Compiler.Lexing;

namespace Compiler.Syntax;

// Recursive descent: one method per grammar rule, each one reading the tokens it needs
// and returning the node it built. Errors are reported, never thrown, and every loop
// consumes at least one token so a broken file can not make the parser spin forever.
public class Parser
{
    private readonly List<Token> tokens;
    private readonly List<Diagnostic> diagnostics = [];
    private int pos;


    public IReadOnlyList<Diagnostic> Diagnostics => diagnostics;


    public Parser(List<Token> tokens)
    {
        this.tokens = tokens;
    }


    // ---- Declarations ----

    public ProgramNode ParseProgram()
    {
        var structs = new List<StructDeclaration>();
        var errors = new List<ErrorDeclaration>();
        var states = new List<StateDeclaration>();
        var functions = new List<FunctionDeclaration>();

        while (Current.type != TokenType.EndOfFile) {
            switch (Current.type) {
                case TokenType.Struct:
                    structs.Add(ParseStructDeclaration());
                    break;
                case TokenType.Error:
                    errors.Add(ParseErrorDeclaration());
                    break;
                case TokenType.State:
                    states.Add(ParseStateDeclaration());
                    break;
                case TokenType.Def:
                case TokenType.External:
                    functions.Add(ParseFunctionDeclaration());
                    break;
                case TokenType.Indent:
                    Error(Current, "unexpected indentation");
                    SkipIndentedBlock();
                    break;
                default:
                    Error(Current, "expected 'struct', 'error', 'state' or 'def'");
                    SkipLine();
                    break;
            }
        }

        return new ProgramNode(structs, errors, states, functions);
    }


    // state uint count
    private StateDeclaration ParseStateDeclaration()
    {
        Expect(TokenType.State, "expected 'state'");
        var variable = ParseTypedName();
        if (variable.Ref != null) Error(variable.Ref, "a state variable cannot be a ref");
        ExpectNewline();
        return new StateDeclaration(variable);
    }


    // error Frozen
    private ErrorDeclaration ParseErrorDeclaration()
    {
        Expect(TokenType.Error, "expected 'error'");
        var name = Expect(TokenType.Identifier, "expected an error name");
        ExpectNewline();
        return new ErrorDeclaration(name);
    }


    // struct Point:
    //     int x
    //     int y
    private StructDeclaration ParseStructDeclaration()
    {
        Expect(TokenType.Struct, "expected 'struct'");
        var name = Expect(TokenType.Identifier, "expected a struct name");
        var fields = new List<TypedName>();

        Expect(TokenType.Colon, "expected ':' after the struct name");
        ExpectNewline();

        if (!Match(TokenType.Indent)) {
            Error(Current, "expected an indented block of fields");
            return new StructDeclaration(name, fields);
        }

        while (Current.type != TokenType.Dedent && Current.type != TokenType.EndOfFile) {
            var field = ParseTypedName();
            if (field.Ref != null) Error(field.Ref, "a struct field cannot be a ref");
            fields.Add(field);
            ExpectNewline();
        }

        Expect(TokenType.Dedent, "expected the end of the struct");
        return new StructDeclaration(name, fields);
    }


    // def (int, int) operate (int x, int y):
    // def uint mul (uint x, uint y):
    // def main ():
    // def uint! withdraw (ref Account account, uint amount):
    // external def uint size (Line line):
    private FunctionDeclaration ParseFunctionDeclaration()
    {
        var external = Current.type == TokenType.External ? Advance() : null;
        Expect(TokenType.Def, "expected 'def'");
        var returnTypes = ParseReturnTypes();
        var fails = Current.type == TokenType.Bang ? Advance() : null;
        var name = Expect(TokenType.Identifier, "expected a function name");
        var parameters = new List<TypedName>();

        Expect(TokenType.LeftParen, "expected '(' after the function name");
        if (Current.type != TokenType.RightParen) {
            do {
                parameters.Add(ParseTypedName());
            } while (Match(TokenType.Comma));
        }
        Expect(TokenType.RightParen, "expected ')' after the parameters");

        var body = ParseBlock();
        return new FunctionDeclaration(external, returnTypes, fails, name, parameters, body);
    }


    // Either a parenthesized list, a single type, or nothing when the name is directly
    // followed by '(' as in "def main ():". A ref would outlive the call that made it,
    // so a function never returns one.
    private List<TypeName> ParseReturnTypes()
    {
        var types = new List<TypeName>();

        if (Match(TokenType.LeftParen)) {
            if (Current.type != TokenType.RightParen) {
                do {
                    SkipRefInReturnType();
                    types.Add(ParseTypeName());
                } while (Match(TokenType.Comma));
            }
            Expect(TokenType.RightParen, "expected ')' after the return types");
        } else {
            SkipRefInReturnType();
            if (Current.type == TokenType.Own || Current.type == TokenType.Identifier && Peek(1).type != TokenType.LeftParen) {
                types.Add(ParseTypeName());
            }
        }

        return types;
    }


    // int, Point, own Node, own Node?
    private TypeName ParseTypeName()
    {
        var own = Current.type == TokenType.Own ? Advance() : null;
        var name = Expect(TokenType.Identifier, "expected a type");
        var optional = Current.type == TokenType.Question ? Advance() : null;
        return new TypeName(own, name, optional);
    }


    private void SkipRefInReturnType()
    {
        if (Current.type != TokenType.Ref) return;
        Error(Current, "a function cannot return a ref");
        Advance();
    }


    // int x, Point start, ref Account account, own Node? left
    private TypedName ParseTypedName()
    {
        var reference = Current.type == TokenType.Ref ? Advance() : null;
        var type = ParseTypeName();
        var name = Expect(TokenType.Identifier, "expected a name after the type");
        return new TypedName(type, name, reference);
    }


    // ---- Statements ----

    // The ':' that opens the block, the line break, then every statement up to the Dedent.
    private List<Statement> ParseBlock()
    {
        var body = new List<Statement>();

        Expect(TokenType.Colon, "expected ':'");
        ExpectNewline();

        if (!Match(TokenType.Indent)) {
            Error(Current, "expected an indented block");
            return body;
        }

        while (Current.type != TokenType.Dedent && Current.type != TokenType.EndOfFile) {
            if (Current.type == TokenType.Indent) {
                Error(Current, "unexpected indentation");
                SkipIndentedBlock();
                continue;
            }
            body.Add(ParseStatement());
        }

        Expect(TokenType.Dedent, "expected the end of the block");
        return body;
    }


    private Statement ParseStatement()
    {
        switch (Current.type) {
            case TokenType.If:
                return ParseIfStatement();
            case TokenType.While:
                return ParseWhileStatement();
            case TokenType.Return:
                return ParseReturnStatement();
        }

        if (LooksLikeDeclaration()) {
            return ParseVariableDeclaration();
        }

        return ParseAssignmentOrCall();
    }


    // A declaration is the only statement that starts with a type then a name: "int x",
    // "ref Point p", "own Node? n".
    private bool LooksLikeDeclaration()
    {
        var i = 0;
        if (Peek(i).type == TokenType.Ref) i++;
        if (Peek(i).type == TokenType.Own) i++;
        if (Peek(i).type != TokenType.Identifier) return false;
        i++;
        if (Peek(i).type == TokenType.Question) i++;
        return Peek(i).type == TokenType.Identifier;
    }


    // int x = 1
    // int s, int d = operate(x, y)
    private VariableDeclaration ParseVariableDeclaration()
    {
        var targets = new List<TypedName>();

        do {
            targets.Add(ParseTypedName());
        } while (Match(TokenType.Comma));

        Expect(TokenType.Assign, "expected '=' after the declaration");
        var value = ParseExpression();

        if (Current.type == TokenType.Catch) {
            value = ParseCatch(value);
        }

        if (Current.type == TokenType.Or && Peek(1).type == TokenType.Colon) {
            Advance();
            return new VariableDeclaration(targets, value, ParseBlock());
        }

        if (value is not CatchBlock) ExpectNewline();
        return new VariableDeclaration(targets, value);
    }


    // The right-hand side of a declaration or an assignment, or a statement on its own: an
    // expression, possibly a call followed by "catch". A catch block ends with its own lines,
    // anything else ends with the line.
    private Expression ParseValue()
    {
        var value = ParseExpression();

        if (Current.type == TokenType.Catch) {
            value = ParseCatch(value);
        }

        if (value is not CatchBlock) ExpectNewline();
        return value;
    }


    // f(x) catch 0
    // f(x) catch err: with a block
    private Expression ParseCatch(Expression target)
    {
        var keyword = Advance();
        var isBlock = Current.type == TokenType.Identifier && Peek(1).type == TokenType.Colon;

        if (target is not CallExpression call) {
            Error(keyword, "'catch' needs a call");
            if (isBlock) { Advance(); ParseBlock(); } else { SkipLine(); }
            return new MissingExpression(keyword);
        }

        if (isBlock) {
            var name = Advance();
            var body = ParseBlock();
            return new CatchBlock(call, keyword, name, body);
        }

        var fallback = ParseExpression();
        return new CatchExpression(call, keyword, fallback);
    }


    // x = 1
    // line.end.x = 1
    // deposit(ref account, 100), try deposit(...), deposit(...) catch err:
    private Statement ParseAssignmentOrCall()
    {
        var start = Current;
        var target = ParseExpression();

        if (target is CallExpression or TryExpression && Current.type != TokenType.Assign) {
            if (Current.type == TokenType.Catch) target = ParseCatch(target);
            if (target is not CatchBlock) ExpectNewline();
            return new CallStatement(target);
        }

        // Without '=' this is not an assignment at all, so the whole line is dropped in one error
        // rather than reported piece by piece.
        if (!Match(TokenType.Assign)) {
            Error(Current, "expected '='");
            SkipLine();
            return new Assignment(target, new MissingExpression(Current));
        }

        if (target is not (NameExpression or FieldExpression or MissingExpression)) {
            Error(start, "only a variable or a field can be assigned");
        }

        var value = ParseValue();
        return new Assignment(target, value);
    }


    // if a: ... elif b: ... else: ...
    // if ref Node c = ref tree.left: ...
    private IfStatement ParseIfStatement()
    {
        var keyword = Expect(TokenType.If, "expected 'if'");
        var arms = new List<IfArm>();
        List<Statement>? elseBody = null;

        arms.Add(ParseIfArm());

        while (Match(TokenType.Elif)) {
            arms.Add(ParseIfArm());
        }

        if (Match(TokenType.Else)) {
            elseBody = ParseBlock();
        }

        return new IfStatement(keyword, arms, elseBody);
    }


    private IfArm ParseIfArm()
    {
        TypedName? binding = null;

        if (LooksLikeDeclaration()) {
            binding = ParseTypedName();
            Expect(TokenType.Assign, "expected '=' after the name to bind");
        }

        var condition = ParseExpression();
        return new IfArm(binding, condition, ParseBlock());
    }


    // while x < 10: ...
    private WhileStatement ParseWhileStatement()
    {
        var keyword = Expect(TokenType.While, "expected 'while'");
        var condition = ParseExpression();
        var body = ParseBlock();
        return new WhileStatement(keyword, condition, body);
    }


    // return
    // return x
    // return a, b
    private ReturnStatement ParseReturnStatement()
    {
        var keyword = Expect(TokenType.Return, "expected 'return'");
        var values = new List<Expression>();

        if (Current.type != TokenType.Newline) {
            do {
                values.Add(ParseExpression());
            } while (Match(TokenType.Comma));
        }

        ExpectNewline();
        return new ReturnStatement(keyword, values);
    }


    // ---- Expressions, from the loosest operator to the tightest ----

    private Expression ParseExpression()
    {
        return ParseOr();
    }


    // "or" followed by ':' is not the operator but the block of a declaration that may find none.
    private Expression ParseOr()
    {
        var left = ParseAnd();

        while (Current.type == TokenType.Or && Peek(1).type != TokenType.Colon) {
            var op = Advance();
            var right = ParseAnd();
            left = new BinaryExpression(op, left, right);
        }

        return left;
    }


    private Expression ParseAnd()
    {
        var left = ParseNot();

        while (Current.type == TokenType.And) {
            var op = Advance();
            var right = ParseNot();
            left = new BinaryExpression(op, left, right);
        }

        return left;
    }


    private Expression ParseNot()
    {
        if (Current.type == TokenType.Not) {
            var op = Advance();
            var operand = ParseNot();
            return new UnaryExpression(op, operand);
        }

        return ParseComparison();
    }


    private Expression ParseComparison()
    {
        var left = ParseAdditive();

        while (Current.type is TokenType.Equal or TokenType.NotEqual or TokenType.Less
               or TokenType.LessEqual or TokenType.Greater or TokenType.GreaterEqual) {
            var op = Advance();
            var right = ParseAdditive();
            left = new BinaryExpression(op, left, right);
        }

        return left;
    }


    private Expression ParseAdditive()
    {
        var left = ParseTerm();

        while (Current.type is TokenType.Plus or TokenType.Minus) {
            var op = Advance();
            var right = ParseTerm();
            left = new BinaryExpression(op, left, right);
        }

        return left;
    }


    private Expression ParseTerm()
    {
        var left = ParseUnary();

        while (Current.type is TokenType.Star or TokenType.Slash or TokenType.Percent) {
            var op = Advance();
            var right = ParseUnary();
            left = new BinaryExpression(op, left, right);
        }

        return left;
    }


    // "ref" reads like a prefix operator, so that "ref line.start" takes the field and not
    // the line. Where it is allowed to appear is for the checker to decide.
    private Expression ParseUnary()
    {
        if (Current.type == TokenType.Minus) {
            var op = Advance();
            var operand = ParseUnary();
            return new UnaryExpression(op, operand);
        }

        if (Current.type == TokenType.Ref) {
            var keyword = Advance();
            var target = ParseUnary();
            return new RefExpression(keyword, target);
        }

        if (Current.type == TokenType.Try) {
            var keyword = Advance();
            var operand = ParsePostfix();
            if (operand is CallExpression call) return new TryExpression(keyword, call);
            Error(keyword, "'try' needs a call");
            return operand;
        }

        if (Current.type == TokenType.Take) {
            var keyword = Advance();
            var place = ParsePostfix();
            return new TakeExpression(keyword, place);
        }

        return ParsePostfix();
    }


    // Calls and field accesses chain on anything: abs(dx), line.end.x, uint(-x).
    private Expression ParsePostfix()
    {
        var expression = ParsePrimary();

        while (true) {
            if (Current.type == TokenType.LeftParen) {
                var leftParen = Advance();
                var arguments = new List<Expression>();

                if (Current.type != TokenType.RightParen) {
                    do {
                        arguments.Add(ParseExpression());
                    } while (Match(TokenType.Comma));
                }

                Expect(TokenType.RightParen, "expected ')' after the arguments");
                expression = new CallExpression(expression, leftParen, arguments);
            } else if (Current.type == TokenType.Dot) {
                Advance();
                var field = Expect(TokenType.Identifier, "expected a field name after '.'");
                expression = new FieldExpression(expression, field);
            } else {
                return expression;
            }
        }
    }


    private Expression ParsePrimary()
    {
        switch (Current.type) {
            case TokenType.Integer:
                return new IntegerLiteral(Advance());
            case TokenType.True:
            case TokenType.False:
                return new BoolLiteral(Advance());
            case TokenType.Identifier:
                return new NameExpression(Advance());
            case TokenType.Error:
                var keyword = Advance();
                Expect(TokenType.Dot, "expected '.' after 'error'");
                return new ErrorLiteral(keyword, Expect(TokenType.Identifier, "expected an error name after 'error.'"));
            case TokenType.None:
                return new NoneLiteral(Advance());
            case TokenType.New:
                return ParseNew();
            case TokenType.LeftParen:
                Advance();
                var inner = ParseExpression();
                Expect(TokenType.RightParen, "expected ')'");
                return inner;
        }

        Error(Current, "expected an expression");
        return new MissingExpression(Current);
    }


    // new Node(1, none, none)
    private Expression ParseNew()
    {
        var keyword = Expect(TokenType.New, "expected 'new'");
        var type = Expect(TokenType.Identifier, "expected a struct name after 'new'");
        var arguments = new List<Expression>();

        Expect(TokenType.LeftParen, "expected '(' after the struct name");
        if (Current.type != TokenType.RightParen) {
            do {
                arguments.Add(ParseExpression());
            } while (Match(TokenType.Comma));
        }
        Expect(TokenType.RightParen, "expected ')' after the values");

        return new NewExpression(keyword, type, arguments);
    }


    // ---- Helpers ----

    private Token Current => tokens[pos];


    // The last token is EndOfFile and is never stepped over, so Current is always valid.
    private Token Peek(int offset)
    {
        var i = Math.Min(pos + offset, tokens.Count - 1);
        return tokens[i];
    }


    private Token Advance()
    {
        var token = tokens[pos];
        if (pos < tokens.Count - 1) pos++;
        return token;
    }


    private bool Match(TokenType type)
    {
        if (Current.type != type) return false;
        Advance();
        return true;
    }


    // On failure the token is not consumed: whoever expected it goes on as if it were there,
    // and the caller's end-of-line check drops whatever is really there.
    private Token Expect(TokenType type, string message)
    {
        if (Current.type == type) return Advance();
        Error(Current, message);
        return Current;
    }


    // Every simple statement ends here. If the line holds more than the statement needed,
    // the rest is reported once and skipped, which keeps a single mistake from producing
    // an error on each of the following tokens.
    private void ExpectNewline()
    {
        if (Match(TokenType.Newline)) return;

        Error(Current, "unexpected token at the end of the line");
        SkipLine();
    }


    // Drop tokens up to the end of the current line, Newline included. Stops short of a
    // Dedent or EndOfFile so that a block is never closed by accident.
    private void SkipLine()
    {
        while (Current.type is not (TokenType.Newline or TokenType.Dedent or TokenType.EndOfFile)) {
            Advance();
        }

        Match(TokenType.Newline);
    }


    // Drop a whole indented block, nested ones included, from its Indent to the matching Dedent.
    private void SkipIndentedBlock()
    {
        var depth = 0;

        do {
            switch (Current.type) {
                case TokenType.Indent:
                    depth++;
                    break;
                case TokenType.Dedent:
                    depth--;
                    break;
                case TokenType.EndOfFile:
                    return;
            }
            Advance();
        } while (depth > 0);
    }


    private void Error(Token at, string message)
    {
        diagnostics.Add(new Diagnostic(at.line, at.column, message));
    }
}
