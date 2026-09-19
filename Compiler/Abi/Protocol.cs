namespace Compiler.Abi;

// The limits and codes every node must agree on. Changing one is a protocol change.
public static class Protocol
{
    // How deep calls may nest. The host sizes a pod's stack from it and the largest frame.
    public const int MaxDepth = 100_000;

    // What a function returns as gas when it gives up: negative, naming the fault.
    public const int AbortDepth = -1;
    public const int AbortGas = -2;
    public const int AbortDivision = -3;
    public const int AbortOverflow = -4;
    public const int AbortMemory = -5;

    // The heap of an instance: blocks come in sizes of 16 bytes doubled up to this many times,
    // each size with its own list of freed blocks. A block that fits no size is refused.
    public const int SizeClasses = 17;

    // How many frames the trace of a failure can hold: the one that raised it, then one per
    // function it went through, which the depth limit bounds.
    public const int TraceCapacity = MaxDepth + 1;
}
