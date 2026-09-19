package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// What one value of each struct of the program takes on the heap. Block sizes double, so a
// value takes the smallest block that holds it: the sizes in between are what show that what
// the host sees live is the block and not the value.
const (
	memorySmallBlock  = 16
	memoryMediumBlock = 32
	memoryLargeBlock  = 64
	memoryExactBlock  = 64
	memoryOverBlock   = 128
)

// memoryPod loads the program with an instance of that many bytes of heap, which is the bound
// the host sets on what one call may hold at once.
func memoryPod(t *testing.T, heap int) (*bluecode.Pod, *bluecode.Instance) {
	t.Helper()

	pod := harness.Build(t, "programs/memory.bc")

	instance, err := pod.NewInstance(heap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })

	return pod, instance
}

// memoryCall runs one function of the program on the instance and fails the test when the pod
// gives up.
func memoryCall(t *testing.T, pod *bluecode.Pod, instance *bluecode.Instance, name string, args, results unsafe.Pointer) {
	t.Helper()

	if _, err := instance.Call(harness.Function(t, pod, name), args, results, 0); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// Each value takes the smallest block that holds it, and what the host reads live is the sum of
// the blocks, not of the values.
func TestMemorySizeClasses(t *testing.T) {
	pod, instance := memoryPod(t, 1<<10)

	sizes := []struct {
		hold  string
		block int
	}{
		{"hold_small", memorySmallBlock},
		{"hold_medium", memoryMediumBlock},
		{"hold_large", memoryLargeBlock},
		{"hold_exact", memoryExactBlock},
		{"hold_over", memoryOverBlock},
	}

	held := 0
	for _, size := range sizes {
		memoryCall(t, pod, instance, size.hold, nil, nil)
		held += size.block

		if live := instance.Live(); live != held {
			t.Errorf("after %s: %d bytes live, expected %d", size.hold, live, held)
		}
	}

	memoryCall(t, pod, instance, "release", nil, nil)
	if live := instance.Live(); live != 0 {
		t.Errorf("after release: %d bytes live", live)
	}
}

// A freed block goes back on the list of its class and the next value of that class takes it
// again, so a heap of exactly one block carries any number of rounds — and holds nothing else.
func TestMemoryReuse(t *testing.T) {
	pod, instance := memoryPod(t, memorySmallBlock)

	rounds := int64(100_000)
	var done int64
	memoryCall(t, pod, instance, "reuse", unsafe.Pointer(&rounds), unsafe.Pointer(&done))

	if done != rounds || instance.Live() != 0 {
		t.Errorf("reuse = %d, %d bytes live", done, instance.Live())
	}

	memoryCall(t, pod, instance, "hold_small", nil, nil)
	memoryCall(t, pod, instance, "release", nil, nil)
	memoryCall(t, pod, instance, "hold_small", nil, nil)
	if live := instance.Live(); live != memorySmallBlock {
		t.Errorf("the block that came back holds a Small again, %d bytes live", live)
	}

	_, err := instance.Call(harness.Function(t, pod, "hold_medium"), nil, nil, 0)

	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Code != -5 {
		t.Fatalf("a list belongs to one class: a Medium does not fit what a Small left, got %v", err)
	}
}

// Every call on the way down holds its block until the one below it returns, so the heap the
// host sized bounds how deep the pod may go. Running out is a fault raised on the line of the
// "new", and the trace names every call that was waiting on it.
func TestMemoryOutOfMemory(t *testing.T) {
	pod, instance := memoryPod(t, 4*memorySmallBlock)
	nest := harness.Function(t, pod, "nest")

	depth := int64(3)
	var reached int64
	if _, err := instance.Call(nest, unsafe.Pointer(&depth), unsafe.Pointer(&reached), 0); err != nil || reached != 4 {
		t.Fatalf("a heap of four blocks carries four nested calls: %d, %v", reached, err)
	}
	if live := instance.Live(); live != 0 {
		t.Errorf("the blocks are freed as the calls return, %d bytes live", live)
	}

	depth = 4
	gasLeft, err := instance.Call(nest, unsafe.Pointer(&depth), unsafe.Pointer(&reached), 0)

	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Name != "out of memory" || failure.Code != -5 {
		t.Fatalf("one call more than the heap holds should give up, got %v", err)
	}
	if gasLeft != failure.Code || reached != 0 {
		t.Errorf("a fault returns its code as the gas and zeroes the results: %d, %d", gasLeft, reached)
	}
	if failure.Source != "memory.bc" || failure.Lost != 0 {
		t.Errorf("failure from %q, %d frames lost", failure.Source, failure.Lost)
	}

	want := []bluecode.Frame{
		{Function: "block", Line: 94, Raised: "out of memory"},
		{Function: "stack_up", Line: 100},
		{Function: "stack_up", Line: 103},
		{Function: "stack_up", Line: 103},
		{Function: "stack_up", Line: 103},
		{Function: "stack_up", Line: 103},
		{Function: "nest", Line: 107},
	}
	if !slices.Equal(failure.Trace, want) {
		t.Errorf("trace = %+v", failure.Trace)
	}
	if live := instance.Live(); live != 0 {
		t.Errorf("the calls free their blocks as they give up, %d bytes live", live)
	}
}

// An instance keeps what its state was given from one call to the next, and the heap the values
// came from with it.
func TestMemoryInstanceKeepsWhatItParked(t *testing.T) {
	pod, instance := memoryPod(t, 1<<10)

	links := int64(3)
	var parked int64
	memoryCall(t, pod, instance, "park", unsafe.Pointer(&links), unsafe.Pointer(&parked))
	memoryCall(t, pod, instance, "park", unsafe.Pointer(&links), unsafe.Pointer(&parked))

	if parked != 2*links || instance.Live() != int(2*links)*memorySmallBlock {
		t.Errorf("%d links parked, %d bytes live", parked, instance.Live())
	}
}

// A call made without an instance runs on one of its own: the state starts zeroed and the heap
// empty, so filling the heap exactly works as often as it is asked for.
func TestMemoryPlainCallStartsEmpty(t *testing.T) {
	pod := harness.Build(t, "programs/memory.bc")
	park := harness.Function(t, pod, "park")

	links := int64(bluecode.DefaultHeapSize / memorySmallBlock)
	var parked int64

	for range 2 {
		if _, err := park.Call(unsafe.Pointer(&links), unsafe.Pointer(&parked), 0); err != nil || parked != links {
			t.Fatalf("park filling the heap = %d, %v", parked, err)
		}
	}

	links++
	_, err := park.Call(unsafe.Pointer(&links), unsafe.Pointer(&parked), 0)

	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Code != -5 {
		t.Fatalf("one link more than the heap holds should give up, got %v", err)
	}
}
