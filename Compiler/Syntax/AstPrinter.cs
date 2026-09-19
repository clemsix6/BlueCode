using System.Text;

namespace Compiler.Syntax;

// Draws the tree with branch lines, one node per line:
//
//   FunctionDeclaration (uint) abs(int x)
//   └── If
//       ├── Binary <
//       │   ├── Name x
//       │   └── IntegerLiteral 0
//       └── Return
//
// Done in two steps: the AST is first turned into plain (label, children) nodes by one
// switch per node family, then drawn. Drawing needs to know whether a node is the last
// child of its parent, which is awkward to do while still walking the AST.
public class AstPrinter
{
    private record Node(string Label, List<Node> Children)
    {
        public Node(string label, params Node[] children) : this(label, children.ToList()) { }
    }


    public static string Print(ProgramNode program)
    {
        var output = new StringBuilder();

        var declarations = program.Structs.Select(Build).Concat(program.Errors.Select(Build))
            .Concat(program.States.Select(Build)).Concat(program.Functions.Select(Build));
        foreach (var declaration in declarations) {
            Draw(declaration, output, "", true, true);
            output.AppendLine();
        }

        return output.ToString();
    }


    // ---- AST → nodes ----

    private static Node Build(StructDeclaration declaration)
    {
        var fields = declaration.Fields.Select(f => new Node($"Field {Describe(f)}"));
        return new Node($"StructDeclaration {declaration.Name.value}", fields.ToList());
    }


    private static Node Build(ErrorDeclaration declaration)
    {
        return new Node($"ErrorDeclaration {declaration.Name.value}");
    }


    private static Node Build(StateDeclaration declaration)
    {
        return new Node($"StateDeclaration {Describe(declaration.Variable)}");
    }


    private static Node Build(FunctionDeclaration declaration)
    {
        var external = declaration.External != null ? "external " : "";
        var returnTypes = string.Join(", ", declaration.ReturnTypes.Select(Describe));
        var fails = declaration.Fails != null ? "!" : "";
        var parameters = string.Join(", ", declaration.Parameters.Select(Describe));
        return new Node($"{external}FunctionDeclaration ({returnTypes}){fails} {declaration.Name.value}({parameters})", Build(declaration.Body));
    }


    private static List<Node> Build(List<Statement> body)
    {
        return body.Select(Build).ToList();
    }


    private static Node Build(Statement statement)
    {
        switch (statement) {
            case VariableDeclaration declaration:
                var parts = new List<Node> { Build(declaration.Value) };
                if (declaration.Otherwise != null) parts.Add(new Node("Or", Build(declaration.Otherwise)));
                return new Node($"VariableDeclaration {string.Join(", ", declaration.Targets.Select(Describe))}", parts);

            case Assignment assignment:
                return new Node("Assignment", Build(assignment.Target), Build(assignment.Value));

            case CallStatement callStatement:
                return new Node("CallStatement", Build(callStatement.Call));

            case IfStatement ifStatement:
                var arms = ifStatement.Arms.Select((arm, i) =>
                    new Node((i == 0 ? "If" : "Elif") + (arm.Binding != null ? " " + Describe(arm.Binding) : ""), Build(arm.Condition), new Node("Body", Build(arm.Body))));
                var children = arms.ToList();
                if (ifStatement.Else != null) {
                    children.Add(new Node("Else", Build(ifStatement.Else)));
                }
                return new Node("IfStatement", children);

            case WhileStatement whileStatement:
                return new Node("While", Build(whileStatement.Condition), new Node("Body", Build(whileStatement.Body)));

            case ReturnStatement returnStatement:
                return new Node("Return", returnStatement.Values.Select(Build).ToList());

            default:
                return new Node($"?? {statement.GetType().Name}");
        }
    }


    private static Node Build(Expression expression)
    {
        switch (expression) {
            case IntegerLiteral literal:
                return new Node($"IntegerLiteral {literal.Value.value}");

            case BoolLiteral literal:
                return new Node($"BoolLiteral {literal.Value.value}");

            case NameExpression name:
                return new Node($"Name {name.Name.value}");

            case UnaryExpression unary:
                return new Node($"Unary {unary.Operator.value}", Build(unary.Operand));

            case BinaryExpression binary:
                return new Node($"Binary {binary.Operator.value}", Build(binary.Left), Build(binary.Right));

            case CallExpression call:
                var children = new List<Node> { Build(call.Callee) };
                children.AddRange(call.Arguments.Select(Build));
                return new Node("Call", children);

            case FieldExpression field:
                return new Node($"Field .{field.Field.value}", Build(field.Target));

            case RefExpression reference:
                return new Node("Ref", Build(reference.Target));

            case ErrorLiteral literal:
                return new Node($"Error .{literal.Name.value}");

            case NewExpression newExpression:
                return new Node($"New {newExpression.Type.value}", newExpression.Arguments.Select(Build).ToList());

            case NoneLiteral:
                return new Node("None");

            case TakeExpression take:
                return new Node("Take", Build(take.Place));

            case TryExpression tryExpression:
                return new Node("Try", Build(tryExpression.Call));

            case CatchExpression catchExpression:
                return new Node("Catch", Build(catchExpression.Call), Build(catchExpression.Fallback));

            case CatchBlock catchBlock:
                return new Node($"CatchBlock {catchBlock.Error.value}", Build(catchBlock.Call), new Node("Body", Build(catchBlock.Body)));

            case MissingExpression:
                return new Node("Missing");

            default:
                return new Node($"?? {expression.GetType().Name}");
        }
    }


    private static string Describe(TypedName typedName)
    {
        var reference = typedName.Ref != null ? "ref " : "";
        return $"{reference}{Describe(typedName.Type)} {typedName.Name.value}";
    }


    private static string Describe(TypeName type)
    {
        var own = type.Own != null ? "own " : "";
        var optional = type.Optional != null ? "?" : "";
        return $"{own}{type.Name.value}{optional}";
    }


    // ---- nodes → text ----

    // prefix is what the parent's ancestors draw on every line below them: "│   " under a
    // branch that continues, "    " under one that is finished.
    private static void Draw(Node node, StringBuilder output, string prefix, bool isLast, bool isRoot)
    {
        if (isRoot) {
            output.AppendLine(node.Label);
        } else {
            output.Append(prefix).Append(isLast ? "└── " : "├── ").AppendLine(node.Label);
            prefix += isLast ? "    " : "│   ";
        }

        for (var i = 0; i < node.Children.Count; i++) {
            Draw(node.Children[i], output, prefix, i == node.Children.Count - 1, false);
        }
    }
}
