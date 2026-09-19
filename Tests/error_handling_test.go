package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// Mirrors of the blocks of error_handling.bc: a function that can fail ends its results with
// the number of its error.
type salaryArgs struct {
	Employer, Employee Account
	Salary             uint64
	Fees               Fees
}

type failingResult struct {
	Left uint64
	Err  int64
}

func TestErrorHandling(t *testing.T) {
	pod := harness.Build(t, "programs/error_handling.bc")
	paySalary := harness.Function(t, pod, "pay_salary")
	fees := Fees{10, 5}

	// Success: a value, no error.
	args := salaryArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, fees}
	var result failingResult
	if _, err := paySalary.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 0); err != nil || result.Left != 3985 || result.Err != 0 {
		t.Errorf("pay_salary = %+v, %v", result, err)
	}

	// The catch block replaces Insufficient with PayrollUnfunded: the trace holds both.
	args = salaryArgs{Account{1, 500, false}, Account{2, 100, false}, 1000, fees}
	_, err := paySalary.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 0)
	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Name != "PayrollUnfunded" || failure.Code != 4 || result.Err != 4 || result.Left != 0 {
		t.Fatalf("pay_salary from an empty account = %+v, %v", result, err)
	}
	want := []bluecode.Frame{
		{Function: "withdraw", Line: 35, Raised: "Insufficient"},
		{Function: "transfer", Line: 55, Raised: ""},
		{Function: "pay_salary", Line: 74, Raised: "PayrollUnfunded"},
	}
	if !slices.Equal(failure.Trace, want) {
		t.Errorf("trace = %+v", failure.Trace)
	}
	text := "PayrollUnfunded\n    error_handling.bc:74 pay_salary\ncaused by Insufficient\n    error_handling.bc:35 withdraw\n    error_handling.bc:55 transfer"
	if failure.Error() != text {
		t.Errorf("printed as:\n%s", failure.Error())
	}
	if args.Employer.Balance != 500 || args.Employee.Balance != 100 {
		t.Errorf("a refused transfer should leave the accounts alone: %+v", args)
	}

	// Any other error is passed on as it is, one frame further.
	args = salaryArgs{Account{1, 5000, false}, Account{2, 100, true}, 1000, fees}
	_, err = paySalary.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 0)
	if !errors.As(err, &failure) || failure.Name != "Frozen" {
		t.Fatalf("pay_salary to a frozen account = %v", err)
	}
	want = []bluecode.Frame{
		{Function: "transfer", Line: 54, Raised: "Frozen"},
		{Function: "pay_salary", Line: 75, Raised: ""},
	}
	if !slices.Equal(failure.Trace, want) {
		t.Errorf("trace = %+v", failure.Trace)
	}

	// A function that cannot fail settles every failure itself.
	leftArgs := struct {
		Account Account
		Amount  uint64
		Fees    Fees
	}{Account{1, 500, false}, 1000, fees}
	var left uint64
	if _, err := harness.Function(t, pod, "left_after").Call(unsafe.Pointer(&leftArgs), unsafe.Pointer(&left), 0); err != nil || left != 0 {
		t.Errorf("left_after = %d, %v", left, err)
	}

	creditArgs := struct {
		Account Account
		Amount  uint64
	}{Account{1, 500, true}, 100}
	var credited bool
	if _, err := harness.Function(t, pod, "credit").Call(unsafe.Pointer(&creditArgs), unsafe.Pointer(&credited), 0); err != nil || credited {
		t.Errorf("credit on a frozen account = %v, %v", credited, err)
	}

	// A failing function without values has the error as its only result.
	var code int64
	_, err = harness.Function(t, pod, "deposit").Call(unsafe.Pointer(&creditArgs), unsafe.Pointer(&code), 0)
	if !errors.As(err, &failure) || failure.Name != "Frozen" || code != 1 || len(failure.Trace) != 1 {
		t.Errorf("deposit on a frozen account = %d, %v", code, err)
	}
}
