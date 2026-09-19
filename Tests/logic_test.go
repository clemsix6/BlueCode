package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// logicGas is the budget every call of this file is given.
const logicGas = 1000

// The lines of logic.bc the division inside a guard sits on.
const (
	logicGuardedWithAndLine = 97
	logicGuardedWithOrLine  = 101
	logicRatioIsLargeLine   = 105
	logicGuardedByACallLine = 109
)

// logicNumbers calls a function taking 64-bit integers and returning a bool, and requires it
// to succeed.
func logicNumbers(t *testing.T, pod *bluecode.Pod, name string, args ...uint64) bool {
	t.Helper()

	var answer bool
	entry := harness.Function(t, pod, name)

	if _, err := entry.Call(unsafe.Pointer(&args[0]), unsafe.Pointer(&answer), logicGas); err != nil {
		t.Fatalf("%s%v: %v", name, args, err)
	}

	return answer
}

// logicBools calls a function taking bools and returning one. A bool is one byte and bools
// pack, so the arguments sit next to each other exactly as this slice does.
func logicBools(t *testing.T, pod *bluecode.Pod, name string, args ...bool) bool {
	t.Helper()

	var answer bool
	entry := harness.Function(t, pod, name)

	if _, err := entry.Call(unsafe.Pointer(&args[0]), unsafe.Pointer(&answer), logicGas); err != nil {
		t.Fatalf("%s%v: %v", name, args, err)
	}

	return answer
}

// logicFailed calls a function that has to give up, and returns the failure and the gas the
// host got back in place of the budget.
func logicFailed(t *testing.T, pod *bluecode.Pod, name string, a, b uint64) (*bluecode.Failure, int64) {
	t.Helper()

	args := [2]uint64{a, b}
	var answer bool
	gasLeft, err := harness.Function(t, pod, name).Call(unsafe.Pointer(&args), unsafe.Pointer(&answer), logicGas)

	var failure *bluecode.Failure
	if err == nil || !errors.As(err, &failure) {
		t.Fatalf("%s(%d, %d) = %t, %v, want a failure", name, a, b, answer, err)
	}

	if answer {
		t.Errorf("%s(%d, %d): the result is %t, a failure zeroes the results block", name, a, b, answer)
	}

	return failure, gasLeft
}

// Which instruction an ordering compiles to is read off the type of its operands, so one set
// of bits orders one way as int and the other way as uint. Equality has no such choice to
// make and answers the same for both.
func TestLogicOrderingFollowsTheType(t *testing.T) {
	pod := harness.Build(t, "programs/logic.bc")

	minusOne := ^uint64(0)

	pairs := []struct {
		signed   string
		unsigned string
		a, b     uint64
		want     bool
	}{
		{"less_signed", "less_unsigned", minusOne, 1, true},
		{"at_most_signed", "at_most_unsigned", minusOne, 1, true},
		{"greater_signed", "greater_unsigned", 1, minusOne, true},
		{"at_least_signed", "at_least_unsigned", 1, minusOne, true},
	}

	for _, pair := range pairs {
		if got := logicNumbers(t, pod, pair.signed, pair.a, pair.b); got != pair.want {
			t.Errorf("%s: %t, want %t", pair.signed, got, pair.want)
		}
		if got := logicNumbers(t, pod, pair.unsigned, pair.a, pair.b); got == pair.want {
			t.Errorf("%s: %t, the same bits must order the other way", pair.unsigned, got)
		}
	}

	if !logicNumbers(t, pod, "equal_ints", minusOne, minusOne) {
		t.Error("equal_ints: the same bits are equal whatever they mean")
	}
	if !logicNumbers(t, pod, "different_ints", 1, 2) {
		t.Error("different_ints(1, 2) is false")
	}
}

// 'not' is looser than every comparison, so it takes the whole comparison to its right and
// never just the value before it.
func TestLogicNotBindsTheWholeComparison(t *testing.T) {
	pod := harness.Build(t, "programs/logic.bc")

	if logicNumbers(t, pod, "not_binds_the_comparison", 3, 3) {
		t.Error("not 3 == 3 is true, so 'not' did not read the comparison")
	}
	if !logicNumbers(t, pod, "not_binds_the_comparison", 3, 4) {
		t.Error("not 3 == 4 is false")
	}

	if !logicBools(t, pod, "double_negation", true) || logicBools(t, pod, "double_negation", false) {
		t.Error("double_negation does not return its argument")
	}
}

// The three operators on their own and in the shapes that make a condition readable.
func TestLogicOperators(t *testing.T) {
	pod := harness.Build(t, "programs/logic.bc")

	numbers := []struct {
		name string
		args []uint64
		want bool
	}{
		{"between", []uint64{5, 1, 10}, true},
		{"between", []uint64{0, 1, 10}, false},
		{"outside", []uint64{0, 1, 10}, true},
		{"same_sign", []uint64{^uint64(0), ^uint64(1)}, true},
		{"same_sign", []uint64{^uint64(0), 2}, false},
	}

	for _, check := range numbers {
		if got := logicNumbers(t, pod, check.name, check.args...); got != check.want {
			t.Errorf("%s%v = %t, want %t", check.name, check.args, got, check.want)
		}
	}

	logicBooleanOperators(t, pod)
}

// logicBooleanOperators checks the functions whose arguments are bools, which are packed one
// byte each.
func logicBooleanOperators(t *testing.T, pod *bluecode.Pod) {
	t.Helper()

	checks := []struct {
		name string
		args []bool
		want bool
	}{
		{"either", []bool{false, true}, true},
		{"either", []bool{false, false}, false},
		{"equal_bools", []bool{true, true}, true},
		{"equal_bools", []bool{true, false}, false},
		{"exclusive_or", []bool{true, false}, true},
		{"exclusive_or", []bool{true, true}, false},
		{"exclusive_or", []bool{false, false}, false},
		{"all_three", []bool{true, true, true}, true},
		{"all_three", []bool{true, false, true}, false},
		{"any_of_three", []bool{false, false, true}, true},
		{"any_of_three", []bool{false, false, false}, false},
	}

	for _, check := range checks {
		if got := logicBools(t, pod, check.name, check.args...); got != check.want {
			t.Errorf("%s%v = %t, want %t", check.name, check.args, got, check.want)
		}
	}
}

// The rule this whole file exists for: 'and' and 'or' evaluate both operands, always. Writing
// the guard on the left of an 'and' does not stop the right from running, so a divisor the
// left side has just ruled out still divides — and faults.
func TestLogicBothOperandsAreEvaluated(t *testing.T) {
	pod := harness.Build(t, "programs/logic.bc")

	for _, guard := range []struct {
		name string
		line int
	}{
		{"guarded_with_and", logicGuardedWithAndLine},
		{"guarded_with_or", logicGuardedWithOrLine},
	} {
		failure, gasLeft := logicFailed(t, pod, guard.name, 10, 0)

		want := []bluecode.Frame{{Function: guard.name, Line: guard.line, Raised: "division by zero"}}
		if failure.Name != "division by zero" || failure.Code != -3 || gasLeft != -3 {
			t.Errorf("%s(10, 0): %q code %d, gas %d", guard.name, failure.Name, failure.Code, gasLeft)
		}
		if !slices.Equal(failure.Trace, want) {
			t.Errorf("%s(10, 0): trace %v, want %v", guard.name, failure.Trace, want)
		}
	}

	if !logicNumbers(t, pod, "guarded_with_and", 10, 2) {
		t.Error("guarded_with_and(10, 2) is false")
	}
	if logicNumbers(t, pod, "guarded_with_and", 1, 2) {
		t.Error("guarded_with_and(1, 2) is true")
	}
	if !logicNumbers(t, pod, "guarded_with_or", 10, 2) {
		t.Error("guarded_with_or(10, 2) is false")
	}
}

// The same holds when the operand is a call: it is made, and its own fault comes back through
// the caller with both frames.
func TestLogicTheOtherOperandIsCalledToo(t *testing.T) {
	pod := harness.Build(t, "programs/logic.bc")

	failure, _ := logicFailed(t, pod, "guarded_by_a_call", 10, 0)

	want := []bluecode.Frame{
		{Function: "ratio_is_large", Line: logicRatioIsLargeLine, Raised: "division by zero"},
		{Function: "guarded_by_a_call", Line: logicGuardedByACallLine},
	}
	if !slices.Equal(failure.Trace, want) {
		t.Errorf("guarded_by_a_call(10, 0): trace %v, want %v", failure.Trace, want)
	}
}

// What does guard is an 'if': it branches, so the division on the far side of the condition is
// never reached.
func TestLogicIfIsWhatGuards(t *testing.T) {
	pod := harness.Build(t, "programs/logic.bc")

	if logicNumbers(t, pod, "guarded_with_if", 10, 0) {
		t.Error("guarded_with_if(10, 0) is true")
	}
	if !logicNumbers(t, pod, "guarded_with_if", 10, 2) {
		t.Error("guarded_with_if(10, 2) is false")
	}
}
