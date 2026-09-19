package tests

import (
	"errors"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// Values that own memory are freed when their owner lets go: the heap of a call is small,
// so a loop that allocates only gets through it if every round frees what it allocated.
func TestOwnedValues(t *testing.T) {
	pod := harness.Build(t, "programs/tree.bc")

	run := func(name string, arg uint64) (uint64, error) {
		t.Helper()
		var result uint64
		_, err := harness.Function(t, pod, name).Call(unsafe.Pointer(&arg), unsafe.Pointer(&result), 0)
		return result, err
	}

	// 200 000 points of 16 bytes: three times the heap, freed one at a time.
	if sum, err := run("churn", 200_000); err != nil || sum != 199_999*200_000/2 {
		t.Errorf("churn = %d, %v", sum, err)
	}

	corners := struct{ A, B int64 }{3, 10}
	var length int64
	if _, err := harness.Function(t, pod, "length").Call(unsafe.Pointer(&corners), unsafe.Pointer(&length), 0); err != nil || length != 7 {
		t.Errorf("length = %d, %v", length, err)
	}

	// 50 000 segments of three blocks, each replacing and freeing the one before.
	if n, err := run("replace", 50_000); err != nil || n != 50_000 {
		t.Errorf("replace = %d, %v", n, err)
	}

	// The heap holds 65 536 points; holding more is a fault raised on the line of the new.
	if n, err := run("hold", 10_000); err != nil || n != 10_001 {
		t.Errorf("hold(10000) = %d, %v", n, err)
	}
	var failure *bluecode.Failure
	_, err := run("hold", 70_000)
	if !errors.As(err, &failure) || failure.Name != "out of memory" || failure.Code != -5 {
		t.Fatalf("hold(70000) should run out of memory, got %v", err)
	}
	if failure.Trace[0] != (bluecode.Frame{Function: "deep", Line: 55, Raised: "out of memory"}) || len(failure.Trace) != 65_537+1 {
		t.Errorf("trace: %d frames, first %+v", len(failure.Trace), failure.Trace[0])
	}
}
