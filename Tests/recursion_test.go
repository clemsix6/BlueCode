package tests

import (
	"errors"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// The depth limit turns runaway recursion into a clean abort. is_even(n) nests n calls:
// twice the limit gives up, half the limit runs, on a stack sized for the limit.
func TestDepthLimit(t *testing.T) {
	pod := harness.Build(t, "programs/recursion.bc")
	isEven := harness.Function(t, pod, "is_even")
	limit := uint64(pod.Manifest().MaxDepth)

	n, result := 2*limit, false
	gasLeft, err := isEven.Call(unsafe.Pointer(&n), unsafe.Pointer(&result), 1_000_000)
	if gasLeft != -1 || result {
		t.Errorf("is_even(%d) should give up with gas -1 and a zero result, got %d and %v", n, gasLeft, result)
	}

	// The fault is raised by the deepest call and passed up by every other one: one frame each.
	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Name != "too deep" || failure.Code != -1 {
		t.Fatalf("a call that went too deep should fail with the too deep fault, got %v", err)
	}
	if len(failure.Trace) != int(limit)+1 || failure.Lost != 0 || failure.Trace[0].Raised != "too deep" || failure.Trace[1].Raised != "" {
		t.Errorf("trace: %d frames, %d lost, first %+v", len(failure.Trace), failure.Lost, failure.Trace[:2])
	}

	n = limit / 2
	gasLeft, err = isEven.Call(unsafe.Pointer(&n), unsafe.Pointer(&result), 1_000_000)
	if gasLeft != 1_000_000 || !result || err != nil {
		t.Errorf("is_even(%d) = %v with gas %d, %v", n, result, gasLeft, err)
	}

	// bank keeps everything in registers, recursion does not: the stacks follow.
	if bank := harness.Build(t, "programs/bank.bc"); bank.StackSize() >= pod.StackSize() {
		t.Errorf("stack sizes: bank %d, recursion %d", bank.StackSize(), pod.StackSize())
	}
}
