package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// BenchmarkCall measures the cost of the boundary alone: a plain call carrying a couple of
// scalar arguments to a scalar result, with no loop or allocation on either side of it.
func BenchmarkCall(b *testing.B) {
	pod := harness.Build(b, "programs/functions.bc")
	fold := harness.Function(b, pod, "fold")

	args, sum := struct{ A, B int64 }{7, 3}, int64(0)

	for b.Loop() {
		if _, err := fold.Call(unsafe.Pointer(&args), unsafe.Pointer(&sum), functionsGas); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFailingCall measures the cost of a call that fails: building the trace as the
// failure is handed up through "try", instead of returning a value across the boundary. The
// allocations it reports are the Go side building the Failure and its trace, the cost a host
// pays for a real failure rather than an artifact of the function chosen.
func BenchmarkFailingCall(b *testing.B) {
	pod := harness.Build(b, "programs/errors.bc")
	bookedPlusOne := harness.Function(b, pod, "booked_plus_one")

	args, result := errorsSeats{Seats: 0, Free: 5}, errorsResult{}

	for b.Loop() {
		if _, err := bookedPlusOne.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 1_000_000); err == nil {
			b.Fatal("booked_plus_one(0, 5) should always fail")
		}
	}
}

// BenchmarkLoop measures a loop the optimiser cannot fold: a checked division every round,
// run against a prime so the loop never finds a divisor and always reaches the limit.
func BenchmarkLoop(b *testing.B) {
	pod := harness.Build(b, "programs/control_flow.bc")
	indexOfDivisor := harness.Function(b, pod, "index_of_divisor")

	args, found := [2]uint64{1000003, 8000}, uint64(0)

	for b.Loop() {
		if _, err := indexOfDivisor.Call(unsafe.Pointer(&args[0]), unsafe.Pointer(&found), 0); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkChurn measures allocation and freeing: many Small values, each freed before the
// next round allocates, so every round takes the same block off the pod's heap.
func BenchmarkChurn(b *testing.B) {
	pod := harness.Build(b, "programs/memory.bc")
	reuse := harness.Function(b, pod, "reuse")

	rounds, done := int64(5000), int64(0)

	for b.Loop() {
		if _, err := reuse.Call(unsafe.Pointer(&rounds), unsafe.Pointer(&done), 0); err != nil {
			b.Fatal(err)
		}
	}
}
