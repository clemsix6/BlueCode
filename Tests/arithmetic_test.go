package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// arithmeticGas is the budget every call of this file is given.
const arithmeticGas = 1000

// The values at the ends of the 64-bit range, which no source literal can write: the host
// passes them in as the bits they are.
const (
	arithmeticMinInt   = uint64(1) << 63 // the smallest int
	arithmeticMaxInt   = arithmeticMinInt - 1
	arithmeticMaxUint  = ^uint64(0)
	arithmeticMinusOne = arithmeticMaxUint // -1 as an int
)

// The lines of arithmetic.bc a fault points at, one per operator.
const (
	arithmeticSignedQuotientLine    = 32
	arithmeticSignedRemainderLine   = 36
	arithmeticUnsignedQuotientLine  = 40
	arithmeticUnsignedRemainderLine = 44
	arithmeticAddSignedLine         = 61
	arithmeticAddUnsignedLine       = 65
	arithmeticSubtractSignedLine    = 69
	arithmeticSubtractUnsignedLine  = 73
	arithmeticMultiplySignedLine    = 77
	arithmeticMultiplyUnsignedLine  = 81
	arithmeticNegateLine            = 85
	arithmeticAverageLine           = 92
)

// arithmeticResult is what one call produced: its single integer result, the gas it gave back,
// and the failure it gave up with, if any.
type arithmeticResult struct {
	value   uint64
	gasLeft int64
	failure *bluecode.Failure
}

// arithmeticCall runs a function whose parameters and single result are all 64-bit integers.
// A signed value is passed and read as its two's complement bits.
func arithmeticCall(t *testing.T, pod *bluecode.Pod, name string, args ...uint64) arithmeticResult {
	t.Helper()

	var value uint64
	entry := harness.Function(t, pod, name)
	gasLeft, err := entry.Call(arithmeticBlock(args), unsafe.Pointer(&value), arithmeticGas)

	var failure *bluecode.Failure
	if err != nil && !errors.As(err, &failure) {
		t.Fatalf("%s: %v is not a Failure", name, err)
	}

	return arithmeticResult{value, gasLeft, failure}
}

// arithmeticSigned is a negative value as the bits the host writes into an argument and reads
// back out of a result; no literal of the language can spell one.
func arithmeticSigned(value int64) uint64 {
	return uint64(value)
}

// arithmeticBlock addresses the arguments; a function taking none is called with no block at
// all, which is what its empty args layout says to do.
func arithmeticBlock(args []uint64) unsafe.Pointer {
	if len(args) == 0 {
		return nil
	}

	return unsafe.Pointer(&args[0])
}

// arithmeticValue checks a call that had to succeed, result and gas both.
func arithmeticValue(t *testing.T, pod *bluecode.Pod, name string, want uint64, args ...uint64) {
	t.Helper()

	got := arithmeticCall(t, pod, name, args...)

	if got.failure != nil {
		t.Fatalf("%s%v: %v", name, args, got.failure)
	}

	if got.value != want {
		t.Errorf("%s%v = %d (%d signed), want %d", name, args, got.value, int64(got.value), want)
	}

	if got.gasLeft != arithmeticGas {
		t.Errorf("%s%v: gas %d, want %d unchanged", name, args, got.gasLeft, arithmeticGas)
	}
}

// arithmeticFault checks a call that had to give up: the results are zeroed, the gas the host
// gets back is the fault's code, and the trace is the one frame that raised it.
func arithmeticFault(t *testing.T, pod *bluecode.Pod, name, fault string, code int64, line int, args ...uint64) {
	t.Helper()

	got := arithmeticCall(t, pod, name, args...)

	if got.failure == nil {
		t.Fatalf("%s%v = %d, want the %s fault", name, args, got.value, fault)
	}

	if got.value != 0 || got.gasLeft != code {
		t.Errorf("%s%v: result %d and gas %d, want 0 and %d", name, args, got.value, got.gasLeft, code)
	}

	want := []bluecode.Frame{{Function: name, Line: line, Raised: fault}}
	if got.failure.Name != fault || got.failure.Code != code || !slices.Equal(got.failure.Trace, want) {
		t.Errorf("%s%v: %q code %d trace %v", name, args, got.failure.Name, got.failure.Code, got.failure.Trace)
	}
}

// Precedence and associativity are fixed by the grammar alone: multiplication and the
// remainder bind tighter than addition, unary minus tighter than both, and every binary
// operator groups to the left.
func TestArithmeticPrecedence(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticValue(t, pod, "precedence", 6)
	arithmeticValue(t, pod, "parentheses", arithmeticSigned(-7))
	arithmeticValue(t, pod, "unary_minus_binds_tighter_than_star", arithmeticSigned(-6), 2, 3)
	arithmeticValue(t, pod, "subtraction_is_left_associative", 5, 10, 3, 2)
	arithmeticValue(t, pod, "division_is_left_associative", 10, 100, 5, 2)
}

// Signed division truncates toward zero and the remainder takes the sign of the dividend, so
// the quotient and the remainder of a pair and of its negation are the negations of each other.
func TestArithmeticSignedDivision(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticValue(t, pod, "signed_quotient", 3, 7, 2)
	arithmeticValue(t, pod, "signed_quotient", arithmeticSigned(-3), arithmeticSigned(-7), 2)
	arithmeticValue(t, pod, "signed_quotient", arithmeticSigned(-3), 7, arithmeticSigned(-2))
	arithmeticValue(t, pod, "signed_quotient", 3, arithmeticSigned(-7), arithmeticSigned(-2))

	arithmeticValue(t, pod, "signed_remainder", 1, 7, 2)
	arithmeticValue(t, pod, "signed_remainder", arithmeticSigned(-1), arithmeticSigned(-7), 2)
	arithmeticValue(t, pod, "signed_remainder", 1, 7, arithmeticSigned(-2))
	arithmeticValue(t, pod, "signed_remainder", arithmeticSigned(-1), arithmeticSigned(-7), arithmeticSigned(-2))
}

// The very same bits divided as unsigned give a completely different answer, which is the
// whole of what the two integer types differ by.
func TestArithmeticUnsignedDivision(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticValue(t, pod, "unsigned_quotient", 3, 7, 2)
	arithmeticValue(t, pod, "unsigned_quotient", arithmeticMaxInt, arithmeticMinusOne, 2)
	arithmeticValue(t, pod, "signed_quotient", 0, arithmeticMinusOne, 2)

	arithmeticValue(t, pod, "unsigned_remainder", 1, 7, 2)
	arithmeticValue(t, pod, "unsigned_remainder", 1, arithmeticMinusOne, 2)
}

// A cast is a reinterpretation of the bits and nothing else: it checks nothing, changes
// nothing, and round trips every value of either type.
func TestArithmeticCasts(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticValue(t, pod, "as_unsigned", arithmeticMaxUint, arithmeticMinusOne)
	arithmeticValue(t, pod, "as_unsigned", arithmeticMinInt, arithmeticMinInt)
	arithmeticValue(t, pod, "as_signed", arithmeticMinusOne, arithmeticMaxUint)
	arithmeticValue(t, pod, "as_signed", arithmeticMinInt, arithmeticMinInt)
}

// Nothing wraps. Every operator that cannot produce the right answer gives up on its own line,
// and the fault names the operator by the line it points at.
func TestArithmeticOverflow(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	fault := func(name string, line int, args ...uint64) {
		t.Helper()
		arithmeticFault(t, pod, name, "overflow", -4, line, args...)
	}

	fault("add_signed", arithmeticAddSignedLine, arithmeticMaxInt, 1)
	fault("add_unsigned", arithmeticAddUnsignedLine, arithmeticMaxUint, 1)
	fault("subtract_signed", arithmeticSubtractSignedLine, arithmeticMinInt, 1)
	fault("multiply_signed", arithmeticMultiplySignedLine, arithmeticMinInt, 2)
	fault("multiply_unsigned", arithmeticMultiplyUnsignedLine, uint64(1)<<32, uint64(1)<<32)
	fault("negate", arithmeticNegateLine, arithmeticMinInt)
}

// Unsigned subtraction borrows into nothing, so it faults where a machine would wrap: zero
// minus one is not the largest unsigned value here, it is no value at all.
func TestArithmeticUnsignedBorrow(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticFault(t, pod, "subtract_unsigned", "overflow", -4, arithmeticSubtractUnsignedLine, 0, 1)
	arithmeticValue(t, pod, "subtract_unsigned", 2, 5, 3)
	arithmeticValue(t, pod, "subtract_signed", arithmeticSigned(-2), 3, 5)
}

// Dividing by zero is guarded for both types and for the remainder as well as the quotient.
func TestArithmeticDivisionByZero(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	fault := func(name string, line int) {
		t.Helper()
		arithmeticFault(t, pod, name, "division by zero", -3, line, 7, 0)
	}

	fault("signed_quotient", arithmeticSignedQuotientLine)
	fault("signed_remainder", arithmeticSignedRemainderLine)
	fault("unsigned_quotient", arithmeticUnsignedQuotientLine)
	fault("unsigned_remainder", arithmeticUnsignedRemainderLine)
}

// The one pair of operands signed division cannot answer for: the smallest int negated is not
// an int. The remainder faults on it too, where C defines it as zero.
func TestArithmeticSmallestIntDivided(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticFault(t, pod, "signed_quotient", "overflow", -4, arithmeticSignedQuotientLine, arithmeticMinInt, arithmeticMinusOne)
	arithmeticFault(t, pod, "signed_remainder", "overflow", -4, arithmeticSignedRemainderLine, arithmeticMinInt, arithmeticMinusOne)

	arithmeticValue(t, pod, "signed_quotient", arithmeticMinInt, arithmeticMinInt, 1)
	arithmeticValue(t, pod, "signed_remainder", 0, arithmeticMinInt, 1)
	arithmeticValue(t, pod, "unsigned_quotient", arithmeticMinInt, arithmeticMinInt, 1)
}

// A fault in a callee is a fault in the caller: the callee's frame is kept and the caller adds
// the line of its call, raiser first and entry last.
func TestArithmeticFaultThroughACaller(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	got := arithmeticCall(t, pod, "average_of_two", 100, 0)

	want := []bluecode.Frame{
		{Function: "signed_quotient", Line: arithmeticSignedQuotientLine, Raised: "division by zero"},
		{Function: "average_of_two", Line: arithmeticAverageLine},
	}
	if got.failure == nil || !slices.Equal(got.failure.Trace, want) {
		t.Fatalf("average_of_two(100, 0): %v", got.failure)
	}

	printed := "division by zero\n    arithmetic.bc:32 signed_quotient\n    arithmetic.bc:92 average_of_two"
	if got.failure.Error() != printed {
		t.Errorf("printed as:\n%s", got.failure.Error())
	}

	if got.failure.Lost != 0 {
		t.Errorf("%d frames lost, the trace is sized for the deepest chain", got.failure.Lost)
	}
}

// Statements after a return in the same block are never emitted, so the division the host asks
// for by passing a zero divisor never happens.
func TestArithmeticDeadCodeAfterAReturn(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	arithmeticValue(t, pod, "after_a_return", 7, 7, 0)
	arithmeticValue(t, pod, "after_a_return", 3, 7, 2)
}

// Nothing charges gas: a call that succeeds gives back every unit it was given, a call with no
// budget at all still runs, and a call that gives up returns the fault's code in its place.
func TestArithmeticGasIsRelayedNotSpent(t *testing.T) {
	pod := harness.Build(t, "programs/arithmetic.bc")

	entry := harness.Function(t, pod, "division_is_left_associative")
	args := []uint64{100, 5, 2}
	var value uint64

	for _, gas := range []int64{0, 1, 1 << 40} {
		gasLeft, err := entry.Call(unsafe.Pointer(&args[0]), unsafe.Pointer(&value), gas)
		if err != nil || gasLeft != gas || value != 10 {
			t.Errorf("with gas %d: result %d, gas %d, %v", gas, value, gasLeft, err)
		}
	}
}
