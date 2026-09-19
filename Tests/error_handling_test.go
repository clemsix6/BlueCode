package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// Mirrors of the blocks of errors.bc: a function that can fail ends its results with the
// number of the error it failed with, zero when it did not.
type errorsSeats struct{ Seats, Free uint64 }

type errorsResult struct {
	Left uint64
	Err  int64
}

// errorsFailure reads the failure a call returned; a call that did not fail fails the test.
func errorsFailure(t *testing.T, err error) *bluecode.Failure {
	t.Helper()

	var failure *bluecode.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("expected a failure, got %v", err)
	}

	return failure
}

// errorsCall runs one function of errors.bc and returns only what went wrong, for the cases
// where the results are read separately.
func errorsCall(t *testing.T, pod *bluecode.Pod, name string, args, results unsafe.Pointer) error {
	t.Helper()

	_, err := harness.Function(t, pod, name).Call(args, results, 0)
	return err
}

// Errors are numbered from one in the order they are declared, and the number of the one a
// call failed with is the last field of its result block, zero on success.
func TestErrorHandlingNumbering(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")
	booked := harness.Function(t, pod, "booked")

	want := []bluecode.ErrorName{{Name: "Refused", Code: 1}, {Name: "Rejected", Code: 2}, {Name: "Abandoned", Code: 3}, {Name: "Retried", Code: 4}}
	if !slices.Equal(pod.Manifest().Errors, want) {
		t.Errorf("errors = %+v", pod.Manifest().Errors)
	}

	fields := booked.Results.Fields
	if last := fields[len(fields)-1]; last.Type != "error" || last.Offset != 8 {
		t.Errorf("booked does not end its results with the error: %+v", fields)
	}

	args, result := errorsSeats{2, 5}, errorsResult{}
	if _, err := booked.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 0); err != nil || result != (errorsResult{3, 0}) {
		t.Errorf("booked(2, 5) = %+v, %v", result, err)
	}

	// A failure read from the error field, not from a gas the pod gave up on: the gas comes
	// back as it went in.
	args = errorsSeats{0, 5}
	gasLeft, err := booked.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 1000)
	failure := errorsFailure(t, err)
	if failure.Code != 1 || gasLeft != 1000 || result != (errorsResult{0, 1}) {
		t.Errorf("booked(0, 5) = %+v, gas %d, %v", result, gasLeft, failure)
	}
}

// "try" hands the failure up with one frame of its own, at the line of the keyword, and binds
// tighter than the arithmetic around it.
func TestErrorHandlingTry(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")

	t.Run("one frame per try", func(t *testing.T) {
		args, result := errorsSeats{0, 5}, errorsResult{}
		failure := errorsFailure(t, errorsCall(t, pod, "booked", unsafe.Pointer(&args), unsafe.Pointer(&result)))

		want := []bluecode.Frame{
			{Function: "reserve", Line: 19, Raised: "Refused"},
			{Function: "book", Line: 26},
			{Function: "booked", Line: 35},
		}
		if failure.Name != "Refused" || !slices.Equal(failure.Trace, want) {
			t.Errorf("booked(0, 5) failed with %s, trace %+v", failure.Name, failure.Trace)
		}
	})

	t.Run("tighter than arithmetic", func(t *testing.T) {
		args, result := errorsSeats{1, 5}, errorsResult{}
		if err := errorsCall(t, pod, "booked_plus_one", unsafe.Pointer(&args), unsafe.Pointer(&result)); err != nil || result != (errorsResult{5, 0}) {
			t.Errorf("booked_plus_one(1, 5) = %+v, %v", result, err)
		}
	})
}

// "catch value" settles the failure on the spot, and the fallback is only worked out on the
// branch where the call failed.
func TestErrorHandlingCatchValue(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")
	share := struct{ Seats, Free, Share uint64 }{1, 6, 0}

	t.Run("the value stands in", func(t *testing.T) {
		args, left := errorsSeats{0, 5}, uint64(1)
		if err := errorsCall(t, pod, "seats_left", unsafe.Pointer(&args), unsafe.Pointer(&left)); err != nil || left != 0 {
			t.Errorf("seats_left(0, 5) = %d, %v", left, err)
		}
	})

	t.Run("the fallback is skipped on the way through", func(t *testing.T) {
		left := uint64(0)
		if err := errorsCall(t, pod, "seats_left_or_share", unsafe.Pointer(&share), unsafe.Pointer(&left)); err != nil || left != 5 {
			t.Errorf("seats_left_or_share(1, 6, 0) = %d, %v", left, err)
		}
	})

	t.Run("and runs when the call failed", func(t *testing.T) {
		share.Seats, share.Free = 0, 6
		left := uint64(0)
		failure := errorsFailure(t, errorsCall(t, pod, "seats_left_or_share", unsafe.Pointer(&share), unsafe.Pointer(&left)))

		if failure.Name != "division by zero" || failure.Code != -3 {
			t.Errorf("the fallback should have divided by zero: %v", failure)
		}
	})
}

// A "catch err:" block reads the error, and leaves the function with a value of its own, with
// another error, or with the one it received.
func TestErrorHandlingCatchBlock(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")

	t.Run("a value of its own", func(t *testing.T) {
		args, left := errorsSeats{0, 5}, uint64(1)
		if err := errorsCall(t, pod, "booked_or_zero", unsafe.Pointer(&args), unsafe.Pointer(&left)); err != nil || left != 0 {
			t.Errorf("booked_or_zero(0, 5) = %d, %v", left, err)
		}
	})

	t.Run("another error for the one it names", func(t *testing.T) {
		args, result := errorsSeats{9, 5}, errorsResult{}
		failure := errorsFailure(t, errorsCall(t, pod, "booked_or_retried", unsafe.Pointer(&args), unsafe.Pointer(&result)))

		want := []bluecode.Frame{
			{Function: "reserve", Line: 21, Raised: "Rejected"},
			{Function: "book", Line: 26},
			{Function: "booked_or_retried", Line: 72, Raised: "Retried"},
		}
		if failure.Name != "Retried" || result.Err != 4 || !slices.Equal(failure.Trace, want) {
			t.Errorf("booked_or_retried(9, 5) = %+v, %v, trace %+v", result, failure.Name, failure.Trace)
		}
	})

	t.Run("the one it received for the rest", func(t *testing.T) {
		args, result := errorsSeats{0, 5}, errorsResult{}
		failure := errorsFailure(t, errorsCall(t, pod, "booked_or_retried", unsafe.Pointer(&args), unsafe.Pointer(&result)))

		want := []bluecode.Frame{
			{Function: "reserve", Line: 19, Raised: "Refused"},
			{Function: "book", Line: 26},
			{Function: "booked_or_retried", Line: 73},
		}
		if failure.Name != "Refused" || !slices.Equal(failure.Trace, want) {
			t.Errorf("booked_or_retried(0, 5) failed with %s, trace %+v", failure.Name, failure.Trace)
		}
	})
}

// Every block that raises instead of relaying chains onto the failure it received, so the
// trace holds a line of causes and prints them one under the other.
func TestErrorHandlingCauses(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")
	args, result := errorsSeats{0, 5}, errorsResult{}
	failure := errorsFailure(t, errorsCall(t, pod, "booked_for_a_group", unsafe.Pointer(&args), unsafe.Pointer(&result)))

	want := []bluecode.Frame{
		{Function: "reserve", Line: 19, Raised: "Refused"},
		{Function: "take_seats", Line: 79, Raised: "Rejected"},
		{Function: "booked_for_a_group", Line: 87, Raised: "Abandoned"},
	}
	if failure.Name != "Abandoned" || failure.Lost != 0 || !slices.Equal(failure.Trace, want) {
		t.Fatalf("booked_for_a_group(0, 5) failed with %s, trace %+v", failure.Name, failure.Trace)
	}

	text := "Abandoned\n    errors.bc:87 booked_for_a_group" +
		"\ncaused by Rejected\n    errors.bc:79 take_seats" +
		"\ncaused by Refused\n    errors.bc:19 reserve"
	if failure.Error() != text {
		t.Errorf("printed as:\n%s", failure.Error())
	}
}

// A block inside a block chains onto the failure the inner call left, not onto the one the
// outer block received, and it only runs at all when the inner call failed.
func TestErrorHandlingNestedBlocks(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")
	args, result := struct{ Seats, Free, Spare uint64 }{0, 5, 2}, errorsResult{}

	if err := errorsCall(t, pod, "booked_or_spare", unsafe.Pointer(&args), unsafe.Pointer(&result)); err != nil || result != (errorsResult{3, 0}) {
		t.Errorf("booked_or_spare(0, 5, 2) = %+v, %v", result, err)
	}

	args.Spare = 0
	failure := errorsFailure(t, errorsCall(t, pod, "booked_or_spare", unsafe.Pointer(&args), unsafe.Pointer(&result)))

	want := []bluecode.Frame{
		{Function: "reserve", Line: 19, Raised: "Refused"},
		{Function: "booked_or_spare", Line: 96, Raised: "Abandoned"},
	}
	if failure.Name != "Abandoned" || !slices.Equal(failure.Trace, want) {
		t.Errorf("booked_or_spare(0, 5, 0) failed with %s, trace %+v", failure.Name, failure.Trace)
	}
}

// Relaying rewinds the trace to the number of frames the block began with: the frames its own
// failures wrote past that point are dropped, and the ones below it are the received failure's.
func TestErrorHandlingRelayRewindsTheTrace(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")
	args, result := errorsSeats{0, 5}, errorsResult{}

	failure := errorsFailure(t, errorsCall(t, pod, "relayed_after_deeper_work", unsafe.Pointer(&args), unsafe.Pointer(&result)))
	deeper := []bluecode.Frame{
		{Function: "reserve", Line: 19, Raised: "Refused"},
		{Function: "relayed_after_deeper_work", Line: 108},
	}
	if failure.Name != "Refused" || !slices.Equal(failure.Trace, deeper) {
		t.Errorf("relayed_after_deeper_work(0, 5) trace = %+v", failure.Trace)
	}

	failure = errorsFailure(t, errorsCall(t, pod, "relayed_after_shallower_work", unsafe.Pointer(&args), unsafe.Pointer(&result)))
	shallower := []bluecode.Frame{
		{Function: "reserve", Line: 19, Raised: "Refused"},
		{Function: "book", Line: 26},
		{Function: "book_group", Line: 30},
		{Function: "relayed_after_shallower_work", Line: 117},
	}
	if failure.Name != "Refused" || !slices.Equal(failure.Trace, shallower) {
		t.Errorf("relayed_after_shallower_work(0, 5) trace = %+v", failure.Trace)
	}
}

// A call with no values is caught on its own line, a declaration of several names takes a
// block like any other, and a fault is never a failure a block can see.
func TestErrorHandlingCallShapes(t *testing.T) {
	pod := harness.Build(t, "programs/errors.bc")

	t.Run("a call with no values, caught", func(t *testing.T) {
		seats, ok := uint64(0), true
		if err := errorsCall(t, pod, "confirmed", unsafe.Pointer(&seats), unsafe.Pointer(&ok)); err != nil || ok {
			t.Errorf("confirmed(0) = %v, %v", ok, err)
		}
	})

	t.Run("a call with no values, tried", func(t *testing.T) {
		seats, result := uint64(0), errorsResult{}
		failure := errorsFailure(t, errorsCall(t, pod, "held", unsafe.Pointer(&seats), unsafe.Pointer(&result)))

		want := []bluecode.Frame{{Function: "confirm", Line: 125, Raised: "Refused"}, {Function: "held", Line: 140}}
		if failure.Name != "Refused" || !slices.Equal(failure.Trace, want) {
			t.Errorf("held(0) failed with %s, trace %+v", failure.Name, failure.Trace)
		}
	})

	t.Run("a declaration of several names", func(t *testing.T) {
		seats := uint64(7)
		var result struct {
			Left, Right uint64
			Err         int64
		}
		if err := errorsCall(t, pod, "halves", unsafe.Pointer(&seats), unsafe.Pointer(&result)); err != nil || result.Left != 3 || result.Right != 4 {
			t.Errorf("halves(7) = %+v, %v", result, err)
		}

		seats = 0
		failure := errorsFailure(t, errorsCall(t, pod, "halves", unsafe.Pointer(&seats), unsafe.Pointer(&result)))
		if failure.Name != "Refused" || result.Left != 0 || result.Right != 0 || result.Err != 1 {
			t.Errorf("halves(0) = %+v, %v", result, failure)
		}
	})

	t.Run("a fault is not caught", func(t *testing.T) {
		args, left := errorsSeats{1, 6}, uint64(0)
		gasLeft, err := harness.Function(t, pod, "divided").Call(unsafe.Pointer(&args), unsafe.Pointer(&left), 1000)
		failure := errorsFailure(t, err)

		want := []bluecode.Frame{
			{Function: "per_head", Line: 162, Raised: "division by zero"},
			{Function: "divided", Line: 167},
		}
		if failure.Code != -3 || gasLeft != -3 || left != 0 || !slices.Equal(failure.Trace, want) {
			t.Errorf("divided(1, 6) = %d, gas %d, %v, trace %+v", left, gasLeft, failure.Name, failure.Trace)
		}
	})
}
