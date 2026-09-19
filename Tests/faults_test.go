package tests

import (
	"errors"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// faultsGas is the budget these calls are given. A call that gives up returns the code of its
// fault in place of it, so the number only has to be distinguishable from one.
const faultsGas = 1_000_000

// faultsCounter mirrors the Counter struct of faults.bc.
type faultsCounter struct {
	Hits uint64 // Hits is raised once before the division that gives up
}

// faultsCounted mirrors the arguments of count_then_divide, whose counter is a ref.
type faultsCounted struct {
	Counter faultsCounter // Counter is the block the pod writes into before it faults
	A       int64         // A is the dividend
	B       int64         // B is the divisor, zero to make the call give up
}

// faultsSettled mirrors the arguments of settled and of relayed.
type faultsSettled struct {
	A      int64 // A is the dividend
	B      int64 // B is the divisor
	Refuse bool  // Refuse asks for the error the function declares instead of a division
}

// TestFaultThroughCallers raises a fault at the bottom of a chain of three. Every caller adds
// its own frame on top of the frames the callee already left, so the trace reads from where
// the fault happened up to the entry the host called.
func TestFaultThroughCallers(t *testing.T) {
	pod := harness.Build(t, "programs/faults.bc")

	args, result := struct{ A, B int64 }{1, 0}, int64(9)
	gasLeft, err := harness.Function(t, pod, "deep_divide").Call(unsafe.Pointer(&args), unsafe.Pointer(&result), faultsGas)
	if gasLeft != -3 || result != 0 {
		t.Errorf("the gas that comes back is the code of the fault and the results are zeroed: %d, %d", gasLeft, result)
	}

	failure := faultsGaveUp(t, err, "division by zero", -3)
	want := []bluecode.Frame{
		{Function: "bottom", Line: 25, Raised: "division by zero"},
		{Function: "middle", Line: 21},
		{Function: "deep_divide", Line: 17},
	}
	if len(failure.Trace) != len(want) || failure.Lost != 0 {
		t.Fatalf("trace of %d frames, %d lost: %+v", len(failure.Trace), failure.Lost, failure.Trace)
	}
	for i, frame := range failure.Trace {
		if frame != want[i] {
			t.Errorf("frame %d is %+v, expected %+v", i, frame, want[i])
		}
	}
}

// TestRefArgumentsSurviveAFault writes through a ref, then faults. The results come back all
// zero, but what the pod already put in the caller's block stays there.
func TestRefArgumentsSurviveAFault(t *testing.T) {
	pod := harness.Build(t, "programs/faults.bc")

	args := faultsCounted{faultsCounter{5}, 1, 0}
	results := struct{ Quotient, Dividend int64 }{9, 9}
	gasLeft, err := harness.Function(t, pod, "count_then_divide").Call(unsafe.Pointer(&args), unsafe.Pointer(&results), faultsGas)

	if gasLeft != -3 || results.Quotient != 0 || results.Dividend != 0 {
		t.Errorf("results after a fault: %+v with %d gas left", results, gasLeft)
	}
	if args.Counter.Hits != 6 {
		t.Errorf("what a ref argument was given before the fault stays written: %+v", args.Counter)
	}

	failure := faultsGaveUp(t, err, "division by zero", -3)
	if len(failure.Trace) != 1 || failure.Trace[0].Line != 31 {
		t.Errorf("the fault was raised where the division is: %+v", failure.Trace)
	}
}

// TestDepthLimit drives the chain of calls to exactly the depth the manifest announces. One
// step below it the call returns; at it, the call that arrives gives up on arrival, and its
// frame names the line its function is declared on rather than any call site.
func TestDepthLimit(t *testing.T) {
	pod := harness.Build(t, "programs/faults.bc")
	nest := harness.Function(t, pod, "nest")
	limit := uint64(pod.Manifest().MaxDepth)

	deepest, reached := limit-1, uint64(0)
	gasLeft, err := nest.Call(unsafe.Pointer(&deepest), unsafe.Pointer(&reached), faultsGas)
	if err != nil || reached != deepest || gasLeft != faultsGas {
		t.Fatalf("nest(%d) = %d with %d gas left, %v", deepest, reached, gasLeft, err)
	}

	tooDeep := limit
	gasLeft, err = nest.Call(unsafe.Pointer(&tooDeep), unsafe.Pointer(&reached), faultsGas)
	if gasLeft != -1 || reached != 0 {
		t.Errorf("nest(%d) should give up with the depth fault: %d, %d", tooDeep, gasLeft, reached)
	}

	failure := faultsGaveUp(t, err, "too deep", -1)
	if len(failure.Trace) != int(limit)+1 || failure.Lost != 0 {
		t.Fatalf("one frame per call plus the one that gave up: %d frames, %d lost", len(failure.Trace), failure.Lost)
	}
	if failure.Trace[0] != (bluecode.Frame{Function: "nest", Line: 35, Raised: "too deep"}) {
		t.Errorf("the deepest frame names the def line of the function that arrived: %+v", failure.Trace[0])
	}
	if failure.Trace[1] != (bluecode.Frame{Function: "nest", Line: 38}) {
		t.Errorf("every caller adds the line of its call: %+v", failure.Trace[1])
	}
}

// TestStackSizeFollowsRecursion compares the stack a recursive pod is given with the one a pod
// whose calls never nest deeply is given: the depth limit is the same, the largest frame is not.
func TestStackSizeFollowsRecursion(t *testing.T) {
	recursive := harness.Build(t, "programs/faults.bc")
	flat := harness.Build(t, "programs/external.bc")

	if flat.StackSize() >= recursive.StackSize() {
		t.Errorf("stack sizes: external %d, faults %d", flat.StackSize(), recursive.StackSize())
	}
}

// TestFaultIsNotAnError is the line between the two ways a call can end. An error the pod
// declares travels in the results and catch and try settle it; a fault travels as a negative
// gas and neither of them ever sees it.
func TestFaultIsNotAnError(t *testing.T) {
	pod := harness.Build(t, "programs/faults.bc")
	settled, result := harness.Function(t, pod, "settled"), int64(9)

	refused := faultsSettled{1, 0, true}
	gasLeft, err := settled.Call(unsafe.Pointer(&refused), unsafe.Pointer(&result), faultsGas)
	if err != nil || result != 0 || gasLeft != faultsGas {
		t.Errorf("catch settles the error the function declares: %d with %d gas left, %v", result, gasLeft, err)
	}

	divides, result := faultsSettled{1, 0, false}, int64(9)
	gasLeft, err = settled.Call(unsafe.Pointer(&divides), unsafe.Pointer(&result), faultsGas)
	if gasLeft != -3 || result != 0 {
		t.Errorf("the same catch does not settle a division by zero: %d with %d gas left", result, gasLeft)
	}
	faultsGaveUp(t, err, "division by zero", -3)
}

// TestTryDoesNotCatchAFault asks the same of try, on a function that hands its own error up
// and lets the fault go past.
func TestTryDoesNotCatchAFault(t *testing.T) {
	pod := harness.Build(t, "programs/faults.bc")
	relayed := harness.Function(t, pod, "relayed")
	var results struct {
		Value int64
		Error int64
	}

	refused := faultsSettled{1, 2, true}
	gasLeft, err := relayed.Call(unsafe.Pointer(&refused), unsafe.Pointer(&results), faultsGas)
	if gasLeft != faultsGas || results.Error != 1 {
		t.Errorf("an error relayed by try lands in the results: %+v with %d gas left", results, gasLeft)
	}
	faultsGaveUp(t, err, "Refused", 1)

	divides := faultsSettled{1, 0, false}
	results.Value, results.Error = 9, 9
	gasLeft, err = relayed.Call(unsafe.Pointer(&divides), unsafe.Pointer(&results), faultsGas)
	if gasLeft != -3 || results != (struct{ Value, Error int64 }{}) {
		t.Errorf("a fault leaves nothing in the results: %+v with %d gas left", results, gasLeft)
	}
	faultsGaveUp(t, err, "division by zero", -3)
}

// faultsGaveUp checks that a call ended the way it was meant to and hands the failure back for
// the frames a test wants to read.
func faultsGaveUp(t *testing.T, err error, name string, code int64) *bluecode.Failure {
	t.Helper()

	var failure *bluecode.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("expected %s, got %v", name, err)
	}

	if failure.Name != name || failure.Code != code || failure.Source != "faults.bc" {
		t.Errorf("ended with %s (%d) from %s, expected %s (%d)", failure.Name, failure.Code, failure.Source, name, code)
	}

	return failure
}
