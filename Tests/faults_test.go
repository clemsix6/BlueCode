package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// A fault is not an error the code can catch: the call gives up, with a trace from the line
// that faulted. The results are zero and the gas names the fault.
func TestFaults(t *testing.T) {
	pod := harness.Build(t, "programs/faults.bc")

	fault := func(name string, a, b uint64) (uint64, int64, *bluecode.Failure) {
		t.Helper()
		args := struct{ A, B uint64 }{a, b}
		var result uint64
		gasLeft, err := harness.Function(t, pod, name).Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 1000)
		var failure *bluecode.Failure
		if err != nil && !errors.As(err, &failure) {
			t.Fatalf("%s: %v is not a Failure", name, err)
		}
		return result, gasLeft, failure
	}

	if r, gas, f := fault("divide", 7, 0); r != 0 || gas != -3 || f == nil || f.Name != "division by zero" || !slices.Equal(f.Trace, []bluecode.Frame{{Function: "divide", Line: 6, Raised: "division by zero"}}) {
		t.Errorf("7 / 0 = %d, gas %d, %v", r, gas, f)
	}
	if r, gas, f := fault("divide", 7, 2); r != 3 || gas != 1000 || f != nil {
		t.Errorf("7 / 2 = %d, gas %d, %v", r, gas, f)
	}
	if r, gas, f := fault("divide_signed", 1<<63, 1<<64-1); r != 0 || gas != -4 || f == nil || f.Name != "overflow" {
		t.Errorf("MinInt64 / -1 = %d, gas %d, %v", int64(r), gas, f)
	}
	if r, _, f := fault("divide_signed", 1<<64-7, 2); int64(r) != -3 || f != nil {
		t.Errorf("-7 / 2 = %d, %v", int64(r), f)
	}
	if r, gas, f := fault("add", 1<<64-1, 1); r != 0 || gas != -4 || f == nil || f.Name != "overflow" || f.Trace[0].Line != 14 {
		t.Errorf("MaxUint64 + 1 = %d, gas %d, %v", r, gas, f)
	}
	if r, gas, f := fault("subtract", 3, 5); r != 0 || gas != -4 || f == nil || f.Trace[0].Line != 18 {
		t.Errorf("3 - 5 = %d, gas %d, %v", r, gas, f)
	}
	if r, _, f := fault("subtract", 5, 3); r != 2 || f != nil {
		t.Errorf("5 - 3 = %d, %v", r, f)
	}
	if r, _, f := fault("multiply", 1<<32, 1<<32); r != 0 || f == nil || f.Name != "overflow" {
		t.Errorf("2^32 * 2^32 = %d, %v", r, f)
	}
	if r, _, f := fault("multiply", 1<<31, 1<<32); r != 1<<63 || f != nil {
		t.Errorf("2^31 * 2^32 = %d, %v", r, f)
	}
	if r, _, f := fault("negate", 1<<63, 0); r != 0 || f == nil || f.Name != "overflow" || f.Trace[0].Line != 26 {
		t.Errorf("-MinInt64 = %d, %v", int64(r), f)
	}
	if r, _, f := fault("negate", 1<<64-7, 0); int64(r) != 7 || f != nil {
		t.Errorf("-(-7) = %d, %v", int64(r), f)
	}

	// The fault goes up through the caller, which adds the line of its call.
	if _, gas, f := fault("average", 100, 0); gas != -3 || f == nil || !slices.Equal(f.Trace, []bluecode.Frame{
		{Function: "divide", Line: 6, Raised: "division by zero"},
		{Function: "average", Line: 31, Raised: ""},
	}) {
		t.Errorf("average(100, 0): gas %d, %v", gas, f)
	}
	if _, _, f := fault("average", 100, 0); f.Error() != "division by zero\n    faults.bc:6 divide\n    faults.bc:31 average" {
		t.Errorf("printed as:\n%s", f.Error())
	}
}
