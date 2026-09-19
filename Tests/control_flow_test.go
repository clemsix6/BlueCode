package tests

import (
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// controlFlowGas is the budget every call of this file is given.
const controlFlowGas = 1_000_000

// controlFlowCall runs a function whose parameters and single result are 64-bit integers, and
// requires it to succeed: nothing in control_flow.bc can fault.
func controlFlowCall(t *testing.T, pod *bluecode.Pod, name string, args ...uint64) uint64 {
	t.Helper()

	var result uint64
	entry := harness.Function(t, pod, name)

	if _, err := entry.Call(unsafe.Pointer(&args[0]), unsafe.Pointer(&result), controlFlowGas); err != nil {
		t.Fatalf("%s%v: %v", name, args, err)
	}

	return result
}

// controlFlowCheck is one call and the answer it owes.
type controlFlowCheck struct {
	name string
	args []uint64
	want uint64
}

// controlFlowRun makes every call of a table and reports the ones that answered wrong.
func controlFlowRun(t *testing.T, pod *bluecode.Pod, checks []controlFlowCheck) {
	t.Helper()

	for _, check := range checks {
		if got := controlFlowCall(t, pod, check.name, check.args...); got != check.want {
			t.Errorf("%s%v = %d, want %d", check.name, check.args, got, check.want)
		}
	}
}

// controlFlowSigned is a negative argument as the bits the host passes.
func controlFlowSigned(value int64) uint64 {
	return uint64(value)
}

// Branching: the elif chain, arms that fall through to the statements after the if, nesting,
// and a condition that is a call.
func TestControlFlowBranches(t *testing.T) {
	pod := harness.Build(t, "programs/control_flow.bc")

	controlFlowRun(t, pod, []controlFlowCheck{
		{"sign", []uint64{controlFlowSigned(-5)}, controlFlowSigned(-1)},
		{"sign", []uint64{0}, 0},
		{"sign", []uint64{7}, 1},

		{"clamp", []uint64{5, 1, 10}, 5},
		{"clamp", []uint64{controlFlowSigned(-3), 1, 10}, 1},
		{"clamp", []uint64{50, 1, 10}, 10},

		{"nested_branches", []uint64{1, 1}, 1},
		{"nested_branches", []uint64{1, controlFlowSigned(-1)}, 2},
		{"nested_branches", []uint64{controlFlowSigned(-1), 1}, 3},
		{"nested_branches", []uint64{controlFlowSigned(-1), controlFlowSigned(-1)}, 4},

		{"elif_chain", []uint64{5}, 1},
		{"elif_chain", []uint64{50}, 2},
		{"elif_chain", []uint64{500}, 3},
		{"elif_chain", []uint64{5000}, 4},

		{"branches_on_a_call", []uint64{10}, 5},
		{"branches_on_a_call", []uint64{7}, 22},
	})
}

// Loops: the condition is evaluated again before every round, a body may declare names that
// live for one round only, and loops nest.
func TestControlFlowLoops(t *testing.T) {
	pod := harness.Build(t, "programs/control_flow.bc")

	controlFlowRun(t, pod, []controlFlowCheck{
		{"factorial", []uint64{0}, 1},
		{"factorial", []uint64{1}, 1},
		{"factorial", []uint64{5}, 120},

		{"gcd", []uint64{12, 18}, 6},
		{"gcd", []uint64{7, 13}, 1},
		{"gcd", []uint64{9, 0}, 9},

		{"product_by_addition", []uint64{3, 4}, 12},
		{"product_by_addition", []uint64{0, 5}, 0},

		{"digit_sum", []uint64{9875}, 29},
		{"digit_sum", []uint64{0}, 0},
	})
}

// There is no 'break': a loop is left by failing its condition, which a flag can do, or by
// returning out of the function, which is the only way out of a loop that never fails its own.
func TestControlFlowLeavingALoop(t *testing.T) {
	pod := harness.Build(t, "programs/control_flow.bc")

	controlFlowRun(t, pod, []controlFlowCheck{
		{"first_multiple_over", []uint64{7, 50}, 56},
		{"first_multiple_over", []uint64{7, 0}, 7},

		{"index_of_divisor", []uint64{15, 10}, 3},
		{"index_of_divisor", []uint64{13, 10}, 0},
	})
}

// A function with no return types needs no return statement: the end of its body is one, and
// the caller carries on afterwards.
func TestControlFlowFunctionWithNoResult(t *testing.T) {
	pod := harness.Build(t, "programs/control_flow.bc")

	controlFlowRun(t, pod, []controlFlowCheck{
		{"calls_a_function_with_no_result", []uint64{5}, 5},
		{"calls_a_function_with_no_result", []uint64{0}, 0},
	})
}

// The shapes the compiler accepts as returning on every path, each of which had to compile for
// this pod to exist at all; the values only confirm which arm ran.
func TestControlFlowShapesThatAlwaysReturn(t *testing.T) {
	pod := harness.Build(t, "programs/control_flow.bc")

	controlFlowRun(t, pod, []controlFlowCheck{
		{"trailing_return", []uint64{4}, 8},

		{"if_else_both_return", []uint64{3}, 3},
		{"if_else_both_return", []uint64{controlFlowSigned(-3)}, 3},

		{"loop_then_return", []uint64{0}, 0},
		{"loop_then_return", []uint64{4}, 10},
	})
}
