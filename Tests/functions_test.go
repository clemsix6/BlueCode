package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// functionsGas is the budget these calls are given. No function of the language spends any of
// it, so the number only has to be recognisable when it comes back.
const functionsGas = 1_000_000

// functionsPair mirrors the Pair struct of functions.bc.
type functionsPair struct {
	Low  int64 // Low is the smaller of the two numbers ordered() was given
	High int64 // High is the larger of them
}

// functionsRemember mirrors the arguments of remember, whose first parameter is a ref: the
// struct sits inline at its full size and the pod writes into the caller's own block.
type functionsRemember struct {
	Pair functionsPair // Pair is the block the pod writes through its ref parameter
	X    int64         // X is the number to write into it
}

// TestReturnTypeForms drives the two shapes that carry values back: one bare type, and a
// parenthesised list of them.
func TestReturnTypeForms(t *testing.T) {
	pod := harness.Build(t, "programs/functions.bc")

	x, same := int64(7), int64(0)
	if _, err := harness.Function(t, pod, "identity").Call(unsafe.Pointer(&x), unsafe.Pointer(&same), functionsGas); err != nil {
		t.Fatal(err)
	}
	if same != 7 {
		t.Errorf("identity(7) = %d", same)
	}

	operands := struct{ X, Y int64 }{10, 3}
	var both struct{ Sum, Difference int64 }
	if _, err := harness.Function(t, pod, "sum_and_difference").Call(unsafe.Pointer(&operands), unsafe.Pointer(&both), functionsGas); err != nil {
		t.Fatal(err)
	}
	if both.Sum != 13 || both.Difference != 7 {
		t.Errorf("sum_and_difference(10, 3) = %+v", both)
	}
}

// TestSignaturesWithoutAValue drives the two shapes that carry none — an empty list of return
// types and no return types at all — and the function that takes no parameters. A block of no
// bytes is passed as nothing.
func TestSignaturesWithoutAValue(t *testing.T) {
	pod := harness.Build(t, "programs/functions.bc")

	forget, x := harness.Function(t, pod, "forget"), int64(9)
	if _, err := forget.Call(unsafe.Pointer(&x), nil, functionsGas); err != nil || forget.Results.Size != 0 {
		t.Errorf("forget: %d bytes of results, %v", forget.Results.Size, err)
	}

	written := functionsRemember{functionsPair{1, 2}, 33}
	remember := harness.Function(t, pod, "remember")
	if _, err := remember.Call(unsafe.Pointer(&written), nil, functionsGas); err != nil || written.Pair.Low != 33 {
		t.Errorf("remember: %+v, %d bytes of results, %v", written.Pair, remember.Results.Size, err)
	}

	answer, result := harness.Function(t, pod, "answer"), int64(0)
	if _, err := answer.Call(nil, unsafe.Pointer(&result), functionsGas); err != nil || result != 42 || answer.Args.Size != 0 {
		t.Errorf("answer() = %d with %d bytes of arguments, %v", result, answer.Args.Size, err)
	}
}

// TestSeveralReturnedValues receives a call's values one declaration at a time, hands another
// call's values straight back, and reads a field off the struct a call returned.
func TestSeveralReturnedValues(t *testing.T) {
	pod := harness.Build(t, "programs/functions.bc")
	operands := struct{ A, B int64 }{10, 3}

	folded := int64(0)
	if _, err := harness.Function(t, pod, "fold").Call(unsafe.Pointer(&operands), unsafe.Pointer(&folded), functionsGas); err != nil {
		t.Fatal(err)
	}
	if folded != 137 {
		t.Errorf("fold(10, 3) = %d", folded)
	}

	var relayed struct{ Sum, Difference int64 }
	if _, err := harness.Function(t, pod, "relay").Call(unsafe.Pointer(&operands), unsafe.Pointer(&relayed), functionsGas); err != nil {
		t.Fatal(err)
	}
	if relayed.Sum != 13 || relayed.Difference != 7 {
		t.Errorf("relay(10, 3) = %+v", relayed)
	}

	larger := int64(0)
	harness.Function(t, pod, "larger").Call(unsafe.Pointer(&operands), unsafe.Pointer(&larger), functionsGas)
	if larger != 10 {
		t.Errorf("larger(10, 3) = %d", larger)
	}
}

// TestRecursiveCalls calls a function that calls itself and two that call each other, the
// second of them declared below the first and internal, so the host cannot reach it.
func TestRecursiveCalls(t *testing.T) {
	pod := harness.Build(t, "programs/functions.bc")

	n, product := uint64(10), uint64(0)
	if _, err := harness.Function(t, pod, "factorial").Call(unsafe.Pointer(&n), unsafe.Pointer(&product), functionsGas); err != nil {
		t.Fatal(err)
	}
	if product != 3628800 {
		t.Errorf("factorial(10) = %d", product)
	}

	even := harness.Function(t, pod, "even")
	for _, run := range []struct {
		n    uint64
		want bool
	}{{8, true}, {7, false}, {0, true}} {
		got := !run.want
		even.Call(unsafe.Pointer(&run.n), unsafe.Pointer(&got), functionsGas)
		if got != run.want {
			t.Errorf("even(%d) = %v", run.n, got)
		}
	}

	if _, err := pod.Function("odd"); err == nil {
		t.Error("odd is internal: the host has no entry to reach it through")
	}
}

// TestValueParametersAreCopies doubles a parameter inside the callee and subtracts the
// caller's own number: the two are still the same, so the callee worked on a copy.
func TestValueParametersAreCopies(t *testing.T) {
	pod := harness.Build(t, "programs/functions.bc")

	x, left := int64(21), int64(0)
	if _, err := harness.Function(t, pod, "copies").Call(unsafe.Pointer(&x), unsafe.Pointer(&left), functionsGas); err != nil {
		t.Fatal(err)
	}
	if left != 21 {
		t.Errorf("copies(21) = %d, the callee's copy leaked into the caller", left)
	}
}

// TestGasIsNeverCharged is the property, not the wish: gas is threaded through every call and
// taken by none, so a long loop gives the budget back untouched and a budget of nothing runs.
func TestGasIsNeverCharged(t *testing.T) {
	pod := harness.Build(t, "programs/functions.bc")
	burn := harness.Function(t, pod, "burn")

	rounds, total := uint64(100_000), uint64(0)
	gasLeft, err := burn.Call(unsafe.Pointer(&rounds), unsafe.Pointer(&total), functionsGas)
	if err != nil || total != 4999950000 || gasLeft != functionsGas {
		t.Errorf("burn(%d) = %d with %d gas left, %v", rounds, total, gasLeft, err)
	}

	gasLeft, err = burn.Call(unsafe.Pointer(&rounds), unsafe.Pointer(&total), 0)
	if err != nil || gasLeft != 0 {
		t.Errorf("a call with no gas at all should still run: %d left, %v", gasLeft, err)
	}
}
