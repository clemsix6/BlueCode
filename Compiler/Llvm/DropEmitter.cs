using Compiler.Abi;
using Compiler.Semantics;
using Type = Compiler.Semantics.Type;

namespace Compiler.Llvm;

// Frees what a value owns: the struct an own points to, with everything that struct owns in
// turn, or the owning fields of a struct held in place. Freeing means giving the block back
// to the heap; nothing runs in the value itself.
public class DropEmitter
{
    private readonly IrBuilder builder;
    private readonly string heap;


    public DropEmitter(IrBuilder builder, string heap)
    {
        this.builder = builder;
        this.heap = heap;
    }


    // Drops the value of the type held at the address.
    public void Drop(Type type, string address)
    {
        switch (type) {
            case OwnType own:
                DropOwned(own.Inner, builder.Emit($"load ptr, ptr {address}"));
                break;

            case StructType { IsMovable: true } structType:
                builder.EmitVoid($"call void @drop.{structType.Name}(ptr {heap}, ptr {address})");
                break;
        }
    }


    // Frees the struct the pointer holds, if it holds one, and whatever that struct owns.
    public void DropOwned(StructType type, string pointer)
    {
        var n = builder.NextLabelNumber();
        var isNone = builder.Emit($"icmp eq ptr {pointer}, null");
        builder.Terminate($"br i1 {isNone}, label %freed{n}, label %free{n}");

        builder.Label($"free{n}");
        if (type.IsMovable) builder.EmitVoid($"call void @drop.{type.Name}(ptr {heap}, ptr {pointer})");
        var (sizeClass, bytes) = Heap.ClassOf(Layout.SizeOf(type));
        builder.EmitVoid($"call void @bc_free(ptr {heap}, ptr {pointer}, i64 {sizeClass}, i64 {bytes})");
        builder.Terminate($"br label %freed{n}");

        builder.Label($"freed{n}");
    }
}
