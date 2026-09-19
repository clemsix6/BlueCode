package bluecode

import (
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"unsafe"
)

// The test pods are built for both CPUs; the object matching this one is picked at run time.
var cpu = map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]

func load(t testing.TB, name string) *Pod {
	t.Helper()

	pod, err := Load(filepath.Join("testdata", name+"."+cpu+".o"), filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pod.Close() })

	return pod
}

func function(t testing.TB, pod *Pod, name string) *Function {
	t.Helper()

	f, err := pod.Function(name)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

// Mirrors of the structs in bank.bc and geometry.bc.
type Account struct {
	Id      uint64
	Balance uint64
	Frozen  bool
}

type Fees struct {
	Flat        uint64
	PerThousand uint64
}

type Point struct{ X, Y int64 }

type Line struct{ Start, End Point }

type transferArgs struct {
	From, To Account // taken by ref: the pod updates them in place
	Amount   uint64
	Fees     Fees
}

func TestTransfer(t *testing.T) {
	pod := load(t, "bank")
	transfer := function(t, pod, "transfer")

	if unsafe.Sizeof(transferArgs{}) != uintptr(transfer.Args.Size) || transfer.Results.Size != 1 {
		t.Fatalf("Go structs do not match the manifest: args %d, results %d", transfer.Args.Size, transfer.Results.Size)
	}
	if !transfer.Args.Fields[0].Ref || !transfer.Args.Fields[1].Ref || transfer.Args.Fields[2].Ref {
		t.Errorf("the manifest should mark from and to as ref: %+v", transfer.Args.Fields)
	}

	args := transferArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	var ok bool

	gasLeft, err := transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&ok), 1_000_000)
	if !ok || args.From.Balance != 3985 || args.To.Balance != 1100 || gasLeft != 1_000_000 || err != nil {
		t.Errorf("transfer = %v, accounts %+v, gas left %d, %v", ok, args, gasLeft, err)
	}

	args.From.Frozen = true
	transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&ok), 1_000_000)
	if ok || args.From.Balance != 3985 || args.To.Balance != 1100 {
		t.Errorf("a transfer from a frozen account should change nothing: %v, %+v", ok, args)
	}
}

// A ref argument is read and written where it sits in the args block.
func TestRefs(t *testing.T) {
	pod := load(t, "refs")

	pair := struct{ A, B int64 }{1, 2}
	function(t, pod, "swap").Call(unsafe.Pointer(&pair), nil, 0)
	if pair.A != 2 || pair.B != 1 {
		t.Errorf("swap(1, 2) = %+v", pair)
	}

	shift := struct {
		Line   Line
		Dx, Dy int64
	}{Line{Point{-3, 5}, Point{4, -6}}, 10, 10}
	function(t, pod, "shift").Call(unsafe.Pointer(&shift), nil, 0)
	if shift.Line != (Line{Point{7, 15}, Point{14, 4}}) {
		t.Errorf("shift = %+v", shift.Line)
	}

	line := Line{Point{-3, -5}, Point{4, 6}}
	function(t, pod, "clamp_start").Call(unsafe.Pointer(&line), nil, 0)
	if line != (Line{Point{0, 0}, Point{4, 6}}) {
		t.Errorf("clamp_start = %+v", line)
	}

	points := struct{ A, B Point }{Point{9, 1}, Point{2, 3}}
	var span int64
	function(t, pod, "order").Call(unsafe.Pointer(&points), unsafe.Pointer(&span), 0)
	if span != 7 || points.A != (Point{2, 3}) || points.B != (Point{9, 1}) {
		t.Errorf("order = %d, %+v", span, points)
	}

	moved := struct {
		Point Point
		Dx    int64
	}{Point{1, 1}, 5}
	var x int64
	function(t, pod, "moved_x").Call(unsafe.Pointer(&moved), unsafe.Pointer(&x), 0)
	if x != 6 || moved.Point != (Point{1, 1}) {
		t.Errorf("moved_x should move a copy: %d, %+v", x, moved.Point)
	}
}

func TestLoopsAndRecursion(t *testing.T) {
	pod := load(t, "primes")

	limit, count := uint64(10000), uint64(0)
	function(t, pod, "count_primes").Call(unsafe.Pointer(&limit), unsafe.Pointer(&count), 0)
	if count != 1229 {
		t.Errorf("count_primes(10000) = %d", count)
	}

	n, prime := uint64(100), uint64(0)
	function(t, pod, "nth_prime").Call(unsafe.Pointer(&n), unsafe.Pointer(&prime), 0)
	if prime != 541 {
		t.Errorf("nth_prime(100) = %d", prime)
	}
}

func TestStructsAndMultipleResults(t *testing.T) {
	pod := load(t, "geometry")

	line := Line{Point{1, 2}, Point{4, 6}}
	var length uint64
	function(t, pod, "size").Call(unsafe.Pointer(&line), unsafe.Pointer(&length), 0)
	if length != 7 {
		t.Errorf("size = %d", length)
	}

	operands := struct{ X, Y int64 }{10, 3}
	var results struct{ Sum, Difference int64 }
	function(t, pod, "operate").Call(unsafe.Pointer(&operands), unsafe.Pointer(&results), 0)
	if results.Sum != 13 || results.Difference != 7 {
		t.Errorf("operate(10, 3) = %+v", results)
	}

	x, absolute := int64(-7), uint64(0)
	function(t, pod, "abs").Call(unsafe.Pointer(&x), unsafe.Pointer(&absolute), 0)
	if absolute != 7 {
		t.Errorf("abs(-7) = %d", absolute)
	}

	// A struct built by the pod comes back laid out like the one Go builds.
	corners := struct{ X1, Y1, X2, Y2 int64 }{1, 2, 4, 6}
	var built Line
	function(t, pod, "segment").Call(unsafe.Pointer(&corners), unsafe.Pointer(&built), 0)
	if built != line {
		t.Errorf("segment = %+v", built)
	}
}

func TestCanonical(t *testing.T) {
	pod := load(t, "bank")
	transfer := function(t, pod, "transfer")
	manifest := pod.Manifest()

	args := transferArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&args)), unsafe.Sizeof(args))

	if err := manifest.Canonical(transfer.Args, data); err != nil {
		t.Errorf("a zeroed struct should be canonical: %v", err)
	}

	data[16] = 2 // from.frozen
	if err := manifest.Canonical(transfer.Args, data); err == nil {
		t.Error("a bool of 2 should be rejected")
	}

	data[16] = 0
	data[17] = 1 // padding after from.frozen
	if err := manifest.Canonical(transfer.Args, data); err == nil {
		t.Error("non-zero padding should be rejected")
	}

	if err := manifest.Canonical(transfer.Args, data[:10]); err == nil {
		t.Error("a short block should be rejected")
	}
}

// The depth limit turns runaway recursion into a clean abort. is_even(n) nests n calls:
// twice the limit gives up, half the limit runs, on a stack sized for the limit.
func TestDepthLimit(t *testing.T) {
	pod := load(t, "recursion")
	isEven := function(t, pod, "is_even")
	limit := uint64(pod.Manifest().MaxDepth)

	n, result := 2*limit, false
	gasLeft, err := isEven.Call(unsafe.Pointer(&n), unsafe.Pointer(&result), 1_000_000)
	if gasLeft != -1 || result {
		t.Errorf("is_even(%d) should give up with gas -1 and a zero result, got %d and %v", n, gasLeft, result)
	}

	// The fault is raised by the deepest call and passed up by every other one: one frame each.
	var failure *Failure
	if !errors.As(err, &failure) || failure.Name != "too deep" || failure.Code != -1 {
		t.Fatalf("a call that went too deep should fail with the too deep fault, got %v", err)
	}
	if len(failure.Trace) != int(limit)+1 || failure.Lost != 0 || failure.Trace[0].Raised != "too deep" || failure.Trace[1].Raised != "" {
		t.Errorf("trace: %d frames, %d lost, first %+v", len(failure.Trace), failure.Lost, failure.Trace[:2])
	}

	n = limit / 2
	gasLeft, err = isEven.Call(unsafe.Pointer(&n), unsafe.Pointer(&result), 1_000_000)
	if gasLeft != 1_000_000 || !result || err != nil {
		t.Errorf("is_even(%d) = %v with gas %d, %v", n, result, gasLeft, err)
	}

	// bank keeps everything in registers, recursion does not: the stacks follow.
	if bank := load(t, "bank"); bank.StackSize() >= pod.StackSize() {
		t.Errorf("stack sizes: bank %d, recursion %d", bank.StackSize(), pod.StackSize())
	}
}

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

func TestErrors(t *testing.T) {
	pod := load(t, "error_handling")
	paySalary := function(t, pod, "pay_salary")
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
	var failure *Failure
	if !errors.As(err, &failure) || failure.Name != "PayrollUnfunded" || failure.Code != 4 || result.Err != 4 || result.Left != 0 {
		t.Fatalf("pay_salary from an empty account = %+v, %v", result, err)
	}
	want := []Frame{{"withdraw", 35, "Insufficient"}, {"transfer", 55, ""}, {"pay_salary", 74, "PayrollUnfunded"}}
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
	want = []Frame{{"transfer", 54, "Frozen"}, {"pay_salary", 75, ""}}
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
	if _, err := function(t, pod, "left_after").Call(unsafe.Pointer(&leftArgs), unsafe.Pointer(&left), 0); err != nil || left != 0 {
		t.Errorf("left_after = %d, %v", left, err)
	}

	creditArgs := struct {
		Account Account
		Amount  uint64
	}{Account{1, 500, true}, 100}
	var credited bool
	if _, err := function(t, pod, "credit").Call(unsafe.Pointer(&creditArgs), unsafe.Pointer(&credited), 0); err != nil || credited {
		t.Errorf("credit on a frozen account = %v, %v", credited, err)
	}

	// A failing function without values has the error as its only result.
	var code int64
	_, err = function(t, pod, "deposit").Call(unsafe.Pointer(&creditArgs), unsafe.Pointer(&code), 0)
	if !errors.As(err, &failure) || failure.Name != "Frozen" || code != 1 || len(failure.Trace) != 1 {
		t.Errorf("deposit on a frozen account = %d, %v", code, err)
	}
}

// A fault is not an error the code can catch: the call gives up, with a trace from the line
// that faulted. The results are zero and the gas names the fault.
func TestFaults(t *testing.T) {
	pod := load(t, "faults")

	fault := func(name string, a, b uint64) (uint64, int64, *Failure) {
		t.Helper()
		args := struct{ A, B uint64 }{a, b}
		var result uint64
		gasLeft, err := function(t, pod, name).Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 1000)
		var failure *Failure
		if err != nil && !errors.As(err, &failure) {
			t.Fatalf("%s: %v is not a Failure", name, err)
		}
		return result, gasLeft, failure
	}

	if r, gas, f := fault("divide", 7, 0); r != 0 || gas != -3 || f == nil || f.Name != "division by zero" || !slices.Equal(f.Trace, []Frame{{"divide", 6, "division by zero"}}) {
		t.Errorf("7 / 0 = %d, gas %d, %v", r, gas, f)
	}
	if r, gas, f := fault("divide", 7, 2); r != 3 || gas != 1000 || f != nil {
		t.Errorf("7 / 2 = %d, gas %d, %v", r, gas, f)
	}
	if r, gas, f := fault("divide_signed", 1<<63, 1<<64-1); r != 0 || gas != -4 || f == nil || f.Name != "overflow" {
		t.Errorf("MinInt64 / -1 = %d, gas %d, %v", int64(r), gas, f)
	}
	if r, _, f := fault("divide_signed", 1<<64-7, 2); int64(r) != -3 || f != nil {
		t.Errorf("-7 / 2 = %d, %v", int64(r), f)
	}
	if r, gas, f := fault("add", 1<<64-1, 1); r != 0 || gas != -4 || f == nil || f.Name != "overflow" || f.Trace[0].Line != 14 {
		t.Errorf("MaxUint64 + 1 = %d, gas %d, %v", r, gas, f)
	}
	if r, gas, f := fault("subtract", 3, 5); r != 0 || gas != -4 || f == nil || f.Trace[0].Line != 18 {
		t.Errorf("3 - 5 = %d, gas %d, %v", r, gas, f)
	}
	if r, _, f := fault("subtract", 5, 3); r != 2 || f != nil {
		t.Errorf("5 - 3 = %d, %v", r, f)
	}
	if r, _, f := fault("multiply", 1<<32, 1<<32); r != 0 || f == nil || f.Name != "overflow" {
		t.Errorf("2^32 * 2^32 = %d, %v", r, f)
	}
	if r, _, f := fault("multiply", 1<<31, 1<<32); r != 1<<63 || f != nil {
		t.Errorf("2^31 * 2^32 = %d, %v", r, f)
	}
	if r, _, f := fault("negate", 1<<63, 0); r != 0 || f == nil || f.Name != "overflow" || f.Trace[0].Line != 26 {
		t.Errorf("-MinInt64 = %d, %v", int64(r), f)
	}
	if r, _, f := fault("negate", 1<<64-7, 0); int64(r) != 7 || f != nil {
		t.Errorf("-(-7) = %d, %v", int64(r), f)
	}

	// The fault goes up through the caller, which adds the line of its call.
	if _, gas, f := fault("average", 100, 0); gas != -3 || f == nil || !slices.Equal(f.Trace, []Frame{{"divide", 6, "division by zero"}, {"average", 31, ""}}) {
		t.Errorf("average(100, 0): gas %d, %v", gas, f)
	}
	if _, _, f := fault("average", 100, 0); f.Error() != "division by zero\n    faults.bc:6 divide\n    faults.bc:31 average" {
		t.Errorf("printed as:\n%s", f.Error())
	}
}

// Values that own memory are freed when their owner lets go: the heap of a call is small,
// so a loop that allocates only gets through it if every round frees what it allocated.
func TestOwnedValues(t *testing.T) {
	pod := load(t, "tree")

	run := func(name string, arg uint64) (uint64, error) {
		t.Helper()
		var result uint64
		_, err := function(t, pod, name).Call(unsafe.Pointer(&arg), unsafe.Pointer(&result), 0)
		return result, err
	}

	// 200 000 points of 16 bytes: three times the heap, freed one at a time.
	if sum, err := run("churn", 200_000); err != nil || sum != 199_999*200_000/2 {
		t.Errorf("churn = %d, %v", sum, err)
	}

	corners := struct{ A, B int64 }{3, 10}
	var length int64
	if _, err := function(t, pod, "length").Call(unsafe.Pointer(&corners), unsafe.Pointer(&length), 0); err != nil || length != 7 {
		t.Errorf("length = %d, %v", length, err)
	}

	// 50 000 segments of three blocks, each replacing and freeing the one before.
	if n, err := run("replace", 50_000); err != nil || n != 50_000 {
		t.Errorf("replace = %d, %v", n, err)
	}

	// The heap holds 65 536 points; holding more is a fault raised on the line of the new.
	if n, err := run("hold", 10_000); err != nil || n != 10_001 {
		t.Errorf("hold(10000) = %d, %v", n, err)
	}
	var failure *Failure
	_, err := run("hold", 70_000)
	if !errors.As(err, &failure) || failure.Name != "out of memory" || failure.Code != -5 {
		t.Fatalf("hold(70000) should run out of memory, got %v", err)
	}
	if failure.Trace[0] != (Frame{"deep", 55, "out of memory"}) || len(failure.Trace) != 65_537+1 {
		t.Errorf("trace: %d frames, first %+v", len(failure.Trace), failure.Trace[0])
	}
}

// An instance keeps the pod's state and heap from one call to the next; nothing that owns
// memory ever crosses to the host, which only sees plain values.
func TestInstanceState(t *testing.T) {
	pod := load(t, "ownership")
	instance, err := pod.NewInstance(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	register := function(t, pod, "register")
	for _, account := range []struct{ Id, Balance uint64 }{{50, 500}, {20, 200}, {80, 800}, {10, 100}, {30, 300}, {20, 250}} {
		account := account
		if _, err := instance.Call(register, unsafe.Pointer(&account), nil, 0); err != nil {
			t.Fatal(err)
		}
	}

	var count uint64
	instance.Call(function(t, pod, "registered"), nil, unsafe.Pointer(&count), 0)
	instance.Call(function(t, pod, "nodes"), nil, unsafe.Pointer(&count), 0)
	if count != 5 {
		t.Errorf("nodes = %d", count)
	}
	if live := instance.Live(); live != 5*64 {
		t.Errorf("five nodes of 40 bytes take 5 blocks of 64: live = %d", live)
	}

	balanceOf := func(id uint64) (bool, uint64) {
		var result struct {
			Found   bool
			Balance uint64
		}
		instance.Call(function(t, pod, "balance"), unsafe.Pointer(&id), unsafe.Pointer(&result), 0)
		return result.Found, result.Balance
	}
	if found, balance := balanceOf(20); !found || balance != 250 {
		t.Errorf("balance(20) = %v, %d", found, balance)
	}
	if found, _ := balanceOf(60); found {
		t.Error("balance(60) should not be found")
	}

	// Pruning the left subtree of 50 frees 20, 10 and 30 at once.
	instance.Call(function(t, pod, "prune_left"), nil, nil, 0)
	instance.Call(function(t, pod, "nodes"), nil, unsafe.Pointer(&count), 0)
	if count != 2 || instance.Live() != 2*64 {
		t.Errorf("after prune_left: %d nodes, %d bytes live", count, instance.Live())
	}
	if found, _ := balanceOf(20); found {
		t.Error("balance(20) should be gone")
	}

	instance.Call(function(t, pod, "clear"), nil, nil, 0)
	instance.Call(function(t, pod, "nodes"), nil, unsafe.Pointer(&count), 0)
	if count != 0 || instance.Live() != 0 {
		t.Errorf("after clear: %d nodes, %d bytes live", count, instance.Live())
	}

	// A call without an instance starts from a zero state every time.
	if _, err := register.Call(unsafe.Pointer(&struct{ Id, Balance uint64 }{1, 1}), nil, 0); err != nil {
		t.Fatal(err)
	}
	function(t, pod, "registered").Call(nil, unsafe.Pointer(&count), 0)
	if count != 0 {
		t.Errorf("a plain call should not keep state, got %d registered", count)
	}
}

// Links are taken out of the state and freed as they go: the heap is empty once the queue is.
func TestQueue(t *testing.T) {
	pod := load(t, "queue")
	instance, err := pod.NewInstance(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	enqueue := function(t, pod, "enqueue")
	dequeue := function(t, pod, "dequeue")
	type transfer struct{ From, To, Amount uint64 }
	type dequeued struct {
		Ok               bool
		From, To, Amount uint64
	}

	for i := uint64(1); i <= 3; i++ {
		args := transfer{i, i * 10, i * 100}
		instance.Call(enqueue, unsafe.Pointer(&args), nil, 0)
	}
	if instance.Live() != 3*32 {
		t.Errorf("three links of 32 bytes: live = %d", instance.Live())
	}

	// The queue is a stack: the last in is the first out.
	var got dequeued
	instance.Call(dequeue, nil, unsafe.Pointer(&got), 0)
	if got != (dequeued{true, 3, 30, 300}) || instance.Live() != 2*32 {
		t.Errorf("dequeue = %+v, live %d", got, instance.Live())
	}

	var pending uint64
	instance.Call(function(t, pod, "pending"), nil, unsafe.Pointer(&pending), 0)
	if pending != 2 {
		t.Errorf("pending = %d", pending)
	}

	instance.Call(function(t, pod, "clear"), nil, nil, 0)
	instance.Call(dequeue, nil, unsafe.Pointer(&got), 0)
	if got.Ok || instance.Live() != 0 {
		t.Errorf("after clear: dequeue = %+v, live %d", got, instance.Live())
	}

	// Two instances of one pod know nothing of each other.
	other, _ := pod.NewInstance(1 << 16)
	defer other.Close()
	args := transfer{9, 9, 9}
	other.Call(enqueue, unsafe.Pointer(&args), nil, 0)
	instance.Call(function(t, pod, "pending"), nil, unsafe.Pointer(&pending), 0)
	other.Call(function(t, pod, "pending"), nil, unsafe.Pointer(&args), 0)
	if pending != 0 || args.From != 1 {
		t.Errorf("instances share state: %d and %d pending", pending, args.From)
	}
}

func TestRejects(t *testing.T) {
	other := map[string]string{"aarch64": "x86_64", "x86_64": "aarch64"}[cpu]
	if _, err := Load(filepath.Join("testdata", "bank."+other+".o"), filepath.Join("testdata", "bank.json")); err == nil {
		t.Error("an object for another CPU should be rejected")
	}

	pod := load(t, "bank")
	if _, err := pod.Function("nothing"); err == nil {
		t.Error("an unknown function should be rejected")
	}
	if _, err := pod.Function("fee_for"); err == nil {
		t.Error("an internal function should be rejected")
	}
}

// Many goroutines in pods at once while the garbage collector runs: preemption signals land
// while the PC is outside Go, and the runtime has to cope with that.
func TestConcurrent(t *testing.T) {
	pod := load(t, "primes")
	countPrimes := function(t, pod, "count_primes")

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

func BenchmarkCall(b *testing.B) {
	pod := load(b, "bank")
	transfer := function(b, pod, "transfer")

	args := transferArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	var ok bool

	// The transfer drains the account it is given; refilling it keeps every call on the same path.
	for b.Loop() {
		args.From.Balance = 5000
		transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&ok), 1_000_000)
	}
}

func BenchmarkFailingCall(b *testing.B) {
	pod := load(b, "error_handling")
	transfer := function(b, pod, "transfer")

	args := salaryArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	var result failingResult

	for b.Loop() {
		args.Employer.Balance = 5000
		transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&result), 1_000_000)
	}
}
