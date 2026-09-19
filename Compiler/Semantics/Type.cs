namespace Compiler.Semantics;

// A type as the checker sees it. Records compare by value, so two IntType are the same type
// wherever they come from, and a StructType is only equal to itself since its field list is
// compared by reference: one instance per declared struct is all it takes.
public abstract record Type
{
    public static readonly Type Int = new IntType();
    public static readonly Type Uint = new UintType();
    public static readonly Type Bool = new BoolType();
    public static readonly Type Error = new ErrorType();
    public static readonly Type None = new NoneType();
    public static readonly Type Invalid = new InvalidType();


    public bool IsNumeric => this is IntType or UintType;


    // Whether a value of this type owns memory of its own: an own, or a struct holding one.
    // Such a value is never copied, only moved, and is freed when its owner lets go of it.
    public bool IsMovable => this is OwnType || this is StructType structType && structType.Fields.Any(f => f.Type.IsMovable);


    // Whether a value of type "other" may be used where this type is expected. Invalid is
    // accepted everywhere so that one mistake is reported once, not again by every parent.
    // An optional own takes none, and an own of the same struct whether optional or not.
    public bool Accepts(Type other)
    {
        if (this is InvalidType || other is InvalidType) return true;

        if (this is OwnType { Optional: true } target) {
            return other is NoneType || other is OwnType source && source.Inner == target.Inner;
        }

        return this == other;
    }
}


public sealed record IntType : Type
{
    public override string ToString() => "int";
}


public sealed record UintType : Type
{
    public override string ToString() => "uint";
}


public sealed record BoolType : Type
{
    public override string ToString() => "bool";
}


// What a failing function gives back in place of its values: one of the errors declared in
// the file, told apart by its number. Only ever compared and returned.
public sealed record ErrorType : Type
{
    public override string ToString() => "error";
}


// The fields are filled in after every struct has been created, so that a field may name a
// struct declared further down the file.
public sealed record StructType(string Name, List<StructField> Fields) : Type
{
    public StructField? FindField(string name)
    {
        return Fields.FirstOrDefault(f => f.Name == name);
    }


    public override string ToString() => Name;
}


public sealed record StructField(string Name, Type Type);


// A struct kept in memory of its own and owned by whoever holds the value: a variable, a field,
// a parameter. Optional lets it hold nothing instead, which is what none is.
public sealed record OwnType(StructType Inner, bool Optional) : Type
{
    public override string ToString() => $"own {Inner.Name}{(Optional ? "?" : "")}";
}


// The type of the literal none, before it meets the optional own it is meant for.
public sealed record NoneType : Type
{
    public override string ToString() => "none";
}


// The type of an expression that is already known to be wrong. Never reported by itself.
public sealed record InvalidType : Type
{
    public override string ToString() => "<error>";
}
