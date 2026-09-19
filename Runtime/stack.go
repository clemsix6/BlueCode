package bluecode

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
)

// A pod runs on a stack of its own, since Go's are small and move. Its depth limit and its
// largest frame bound what any call can need, so that is the size, plus a guard page under
// it: an overflow, which the compiler already rules out, then faults instead of silently
// overwriting whatever lies below. Under the guard page sit the trace, where a call that
// fails leaves the frames it went through, and an instance of the pod's own, wiped before
// every call: the state and heap of a call made without an instance of the host's.
type stack struct {
	memory   []byte
	top      uintptr
	instance []byte // the head of the mapping, laid out as instanceLayout says
	trace    []byte // right after it
}

// stackPool hands out stacks of one size, one per call in flight. Stacks the pool lets go
// of are unmapped by the garbage collector.
type stackPool struct {
	pool sync.Pool
}

func newStackPool(stackSize, traceSize int, layout instanceLayout) *stackPool {
	p := &stackPool{}
	p.pool.New = func() any { return newStack(stackSize, traceSize, layout) }
	return p
}

func (p *stackPool) get() *stack {
	return p.pool.Get().(*stack)
}

func (p *stackPool) put(s *stack) {
	p.pool.Put(s)
}

// The mapping is the instance, the trace, the guard page, then the stack. Pages are only
// backed once touched, so a heap and a trace that stay unused and a stack that stays shallow
// cost nothing.
func newStack(stackSize, traceSize int, layout instanceLayout) *stack {
	page := os.Getpagesize()
	instanceSize := wholePages(layout.size())

	memory, err := syscall.Mmap(-1, 0, instanceSize+traceSize+page+stackSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		panic(fmt.Errorf("bluecode: stack: %w", err))
	}

	guard := instanceSize + traceSize
	if err := syscall.Mprotect(memory[guard:guard+page], syscall.PROT_NONE); err != nil {
		panic(fmt.Errorf("bluecode: guard page: %w", err))
	}

	s := &stack{memory, addressOf(memory) + uintptr(len(memory)), memory[:instanceSize], memory[instanceSize:guard]}
	layout.reset(s.instance)
	runtime.AddCleanup(s, func(m []byte) { syscall.Munmap(m) }, memory)
	return s
}

// stackSize is what a pod can need at most: the entry, then nested calls down to the one that
// gives up on arrival, each at most the largest frame plus a return address; whole pages.
func stackSize(maxDepth, maxFrame int) int {
	return wholePages((maxDepth + 2) * (maxFrame + 16))
}

// traceSize is what the longest trace can take: its count, then a frame for the function that
// raised the failure and one for each function it went through, which the depth limit bounds.
func traceSize(maxDepth int) int {
	return wholePages(traceHeader + (maxDepth+1)*frameSize)
}

func wholePages(size int) int {
	page := os.Getpagesize()
	return (size + page - 1) / page * page
}
