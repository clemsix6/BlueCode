package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

func BenchmarkCall(b *testing.B) {
	pod := harness.Build(b, "programs/bank.bc")
	transfer := harness.Function(b, pod, "transfer")

	args := transferArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	var ok bool

	// The transfer drains the account it is given; refilling it keeps every call on the same path.
	for b.Loop() {
		args.From.Balance = 5000
		transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&ok), 1_000_000)
	}
}

func BenchmarkFailingCall(b *testing.B) {
	pod := harness.Build(b, "programs/error_handling.bc")
	transfer := harness.Function(b, pod, "transfer")

	args := salaryArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	var result failingResult

	for b.Loop() {
		args.Employer.Balance = 5000
		transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 1_000_000)
	}
}

// BenchmarkCountPrimes measures checked arithmetic and loops: scanning for primes up to the
// limit the tests use.
func BenchmarkCountPrimes(b *testing.B) {
	pod := harness.Build(b, "programs/primes.bc")
	countPrimes := harness.Function(b, pod, "count_primes")

	limit, count := uint64(10000), uint64(0)
	for b.Loop() {
		countPrimes.Call(unsafe.Pointer(&limit), unsafe.Pointer(&count), 0)
	}
}

// BenchmarkChurn measures allocation and freeing: a few thousand points, each freed at the end
// of the loop body that allocated it.
func BenchmarkChurn(b *testing.B) {
	pod := harness.Build(b, "programs/tree.bc")
	churn := harness.Function(b, pod, "churn")

	n, sum := uint64(5000), int64(0)
	for b.Loop() {
		churn.Call(unsafe.Pointer(&n), unsafe.Pointer(&sum), 0)
	}
}
