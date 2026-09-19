namespace Compiler.Abi;

// How the heap of an instance is laid out, which the host sets up and the pod runs. The header
// holds where fresh memory begins and ends, how many bytes are in use, and one list of freed
// blocks per size; the memory the blocks are cut from follows the header.
public static class Heap
{
    public const int HeaderSize = 3 * 8 + Protocol.SizeClasses * 8;
    public const int MinimumBlock = 16;


    // The size class a value of this many bytes is allocated in, and the bytes a block of that
    // class takes. Sizes double from the minimum, so a block wastes less than half of itself.
    public static (int Class, int Bytes) ClassOf(int size)
    {
        var sizeClass = 0;
        var bytes = MinimumBlock;

        while (bytes < size) {
            sizeClass++;
            bytes *= 2;
        }

        if (sizeClass >= Protocol.SizeClasses) {
            throw new InvalidOperationException($"no size class holds {size} bytes");
        }

        return (sizeClass, bytes);
    }
}
