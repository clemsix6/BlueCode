package tests

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

func TestLoopsAndRecursion(t *testing.T) {
	pod := harness.Build(t, "programs/primes.bc")

	limit, count := uint64(10000), uint64(0)
	harness.Function(t, pod, "count_primes").Call(unsafe.Pointer(&limit), unsafe.Pointer(&count), 0)
	if count != 1229 {
		t.Errorf("count_primes(10000) = %d", count)
	}

	n, prime := uint64(100), uint64(0)
	harness.Function(t, pod, "nth_prime").Call(unsafe.Pointer(&n), unsafe.Pointer(&prime), 0)
	if prime != 541 {
		t.Errorf("nth_prime(100) = %d", prime)
	}
}

// Many goroutines in pods at once while the garbage collector runs: preemption signals land
// while the PC is outside Go, and the runtime has to cope with that.
func TestConcurrent(t *testing.T) {
	pod := harness.Build(t, "programs/primes.bc")
	countPrimes := harness.Function(t, pod, "count_primes")

	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 200 {
				limit, count := uint64(10000), uint64(0)
				countPrimes.Call(unsafe.Pointer(&limit), unsafe.Pointer(&count), 0)
				if count != 1229 {
					t.Errorf("count_primes(10000) = %d", count)
				}
				_ = make([]byte, 1<<16)
			}
		})
	}

	for range 20 {
		runtime.GC()
	}
	group.Wait()
}
