package bluecode

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DefaultHeapSize is the heap of the instance a plain Call runs on.
const DefaultHeapSize = 1 << 20

// The heap header the compiler lays out (Compiler/Abi/Heap.cs): where fresh memory begins and
// ends, the bytes in use, then one list of freed blocks per size class.
const (
	heapHeader  = 3*8 + sizeClasses*8
	sizeClasses = 17
	heapLimitAt = 8
	heapLiveAt  = 16
)

// instanceLayout is how an instance sits in memory: the pod's state, then the heap header
// aligned like it, then the memory the heap cuts blocks from.
type instanceLayout struct {
	state int // bytes of state
	heap  int // bytes the heap may hand out
}

func (l instanceLayout) heapAt() int {
	return (l.state + 7) &^ 7
}

func (l instanceLayout) size() int {
	return l.heapAt() + heapHeader + l.heap
}

// reset wipes the state and the header, and tells the heap how much memory it has.
func (l instanceLayout) reset(memory []byte) {
	clear(memory[:l.heapAt()+heapHeader])
	*(*int64)(unsafe.Pointer(&memory[l.heapAt()+heapLimitAt])) = int64(l.heap)
}

// untouched tells whether the instance is as reset left it, so that resetting it again can be
// skipped: no state to speak of, and no block ever cut from the heap.
func (l instanceLayout) untouched(memory []byte) bool {
	return l.state == 0 && *(*int64)(unsafe.Pointer(&memory[l.heapAt()])) == 0
}

func (l instanceLayout) live(memory []byte) int {
	return int(*(*int64)(unsafe.Pointer(&memory[l.heapAt()+heapLiveAt])))
}

// Instance is memory of the pod's own that outlives calls: the state its "state" variables
// live in, and the heap its "own" values are allocated from. What one call builds, the next
// finds. An instance is used by one call at a time.
type Instance struct {
	pod    *Pod
	memory []byte
	layout instanceLayout
}

// NewInstance creates an instance with a zeroed state and a heap of that many bytes.
func (p *Pod) NewInstance(heapSize int) (*Instance, error) {
	layout := instanceLayout{state: p.manifest.State.Size, heap: heapSize}

	memory, err := syscall.Mmap(-1, 0, wholePages(layout.size()), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("bluecode: instance: %w", err)
	}

	layout.reset(memory)
	return &Instance{p, memory, layout}, nil
}

// Call runs the function on this instance, otherwise like Function.Call.
func (i *Instance) Call(f *Function, args, results unsafe.Pointer, gas int64) (int64, error) {
	if f.pod != i.pod {
		return 0, fmt.Errorf("bluecode: %s belongs to another pod", f.Name)
	}

	stack := f.pod.stacks.get()
	left, err := f.call(stack, addressOf(i.memory), args, results, gas)
	f.pod.stacks.put(stack)
	return left, err
}

// Live is how many bytes of the heap are held by own values right now.
func (i *Instance) Live() int {
	return i.layout.live(i.memory)
}

// Close releases the instance. Its state and everything it owned are gone; the pod's
// functions must not be called on it afterwards.
func (i *Instance) Close() error {
	if i.memory == nil {
		return nil
	}

	err := syscall.Munmap(i.memory)
	i.memory = nil
	return err
}
