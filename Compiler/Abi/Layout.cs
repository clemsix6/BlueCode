using Compiler.Semantics;
using Type = Compiler.Semantics.Type;

namespace Compiler.Abi;

// Where values sit in memory, by the natural alignment rule that LLVM, C and Go all apply:
// a field starts at the next multiple of its alignment, a struct is aligned like its widest
// field and its size is rounded up to that alignment. The host lays its blocks out the same
// way, which is why the pod can read them without any decoding.
public static class Layout
{
    public static int SizeOf(Type type)
    {
        return type switch {
            IntType or UintType or ErrorType or OwnType => 8,
            BoolType => 1,
            StructType structType => Place(structType.Fields.Select(f => f.Type)).Size,
            _ => throw new InvalidOperationException($"no layout for {type}"),
        };
    }


    public static int AlignmentOf(Type type)
    {
        return type switch {
            IntType or UintType or ErrorType or OwnType => 8,
            BoolType => 1,
            StructType structType => structType.Fields.Select(f => AlignmentOf(f.Type)).DefaultIfEmpty(1).Max(),
            _ => throw new InvalidOperationException($"no layout for {type}"),
        };
    }


    // Lays values out one after the other, as the fields of a struct.
    public static (List<int> Offsets, int Size) Place(IEnumerable<Type> types)
    {
        var offsets = new List<int>();
        var size = 0;
        var alignment = 1;

        foreach (var type in types) {
            var typeAlignment = AlignmentOf(type);
            size = RoundUp(size, typeAlignment);
            offsets.Add(size);
            size += SizeOf(type);
            alignment = Math.Max(alignment, typeAlignment);
        }

        return (offsets, RoundUp(size, alignment));
    }


    private static int RoundUp(int value, int multiple)
    {
        return (value + multiple - 1) / multiple * multiple;
    }
}
