package bluecode

import "unsafe"

// call jumps into a pod entry with the C calling convention, on the given stack, and returns
// what the entry returns. Implemented in assembly for each CPU. It bypasses the Go runtime
// entirely, which is only safe because a pod never blocks, allocates or calls back into Go.
//
//go:noescape
func call(entry, args, results, trace, instance, gas, stack uintptr) int64

// flushInstructionCache makes freshly written code visible to instruction fetch. A no-op on
// x86, which keeps its caches coherent; on ARM the data and instruction caches are not.
//
//go:noescape
func flushInstructionCache(start, end uintptr)

func addressOf(memory []byte) uintptr {
	return uintptr(unsafe.Pointer(&memory[0]))
}
