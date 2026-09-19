package tests

import (
	"errors"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// ownershipBlock is what one Point or one Link of the program takes on the heap: both fit the
// smallest block the allocator cuts, so a heap of one block leaves no room for a value that
// should have been freed.
const ownershipBlock = 16

// ownershipCorners is the pair of numbers every function of the program that builds a point
// takes, and seven is the sum it answers with.
type ownershipCorners struct {
	X, Y int64
}

// ownershipFound is what a function answers when the chain may hold nothing.
type ownershipFound struct {
	Found bool
	Value int64
}

// ownershipPod loads the program with an instance of that many bytes of heap.
func ownershipPod(t *testing.T, heap int) (*bluecode.Pod, *bluecode.Instance) {
	t.Helper()

	pod := harness.Build(t, "programs/ownership.bc")

	instance, err := pod.NewInstance(heap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })

	return pod, instance
}

// ownershipCall runs one function of the program on the instance and fails the test when the
// pod gives up, which for this program means a value outlived its owner.
func ownershipCall(t *testing.T, pod *bluecode.Pod, instance *bluecode.Instance, name string, args, results unsafe.Pointer) {
	t.Helper()

	if _, err := instance.Call(harness.Function(t, pod, name), args, results, 0); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// A value given to a function is the callee's: it frees it unless it hands it on in turn, and
// a value given to another name dies under the old one. The heap holds a single block, so a
// value left behind would stop the call after it.
func TestOwnershipHandOver(t *testing.T) {
	pod, instance := ownershipPod(t, ownershipBlock)
	corners := ownershipCorners{3, 4}

	for _, name := range []string{"handed_to_a_callee", "handed_on_twice", "moved_into_a_name", "freed_at_the_exit"} {
		var sum int64
		ownershipCall(t, pod, instance, name, unsafe.Pointer(&corners), unsafe.Pointer(&sum))

		if sum != corners.X+corners.Y || instance.Live() != 0 {
			t.Errorf("%s = %d, %d bytes live", name, sum, instance.Live())
		}
	}
}

// A branch that leaves the function may move the value; the code after the "if" still owns it
// and moves it in its turn.
func TestOwnershipMoveInAReturningBranch(t *testing.T) {
	pod, instance := ownershipPod(t, ownershipBlock)

	run := func(early bool) int64 {
		t.Helper()

		args := struct {
			X, Y  int64
			Early bool
		}{3, 4, early}

		var sum int64
		ownershipCall(t, pod, instance, "moved_in_a_returning_branch", unsafe.Pointer(&args), unsafe.Pointer(&sum))
		return sum
	}

	if sum := run(true); sum != 0 {
		t.Errorf("the branch that leaves early answers 0, got %d", sum)
	}
	if sum := run(false); sum != 7 {
		t.Errorf("the branch that keeps the value answers its sum, got %d", sum)
	}
	if live := instance.Live(); live != 0 {
		t.Errorf("both branches free the value, %d bytes live", live)
	}
}

// A value declared inside a loop body belongs to the round and not to the function: one block
// carries as many rounds as are asked for.
func TestOwnershipFreedEveryRound(t *testing.T) {
	pod, instance := ownershipPod(t, ownershipBlock)

	rounds := int64(100_000)
	var sum int64
	ownershipCall(t, pod, instance, "freed_every_round", unsafe.Pointer(&rounds), unsafe.Pointer(&sum))

	if sum != rounds || instance.Live() != 0 {
		t.Errorf("freed_every_round = %d, %d bytes live", sum, instance.Live())
	}
}

// The chain lives in the state. Pushing takes the old head out of it rather than reading it,
// taking a value out and putting it back frees nothing, and assigning the place frees the whole
// chain that was there.
func TestOwnershipChain(t *testing.T) {
	pod, instance := ownershipPod(t, 1<<10)

	head := func() ownershipFound {
		t.Helper()
		var got ownershipFound
		ownershipCall(t, pod, instance, "head_value", nil, unsafe.Pointer(&got))
		return got
	}
	length := func() int64 {
		t.Helper()
		var n int64
		ownershipCall(t, pod, instance, "chain_length", nil, unsafe.Pointer(&n))
		return n
	}

	for _, value := range []int64{1, 2, 3, 4, 5} {
		ownershipCall(t, pod, instance, "push", unsafe.Pointer(&value), nil)
	}
	if length() != 5 || instance.Live() != 5*ownershipBlock {
		t.Fatalf("five links: length %d, %d bytes live", length(), instance.Live())
	}
	if got := head(); got != (ownershipFound{true, 5}) {
		t.Errorf("head = %+v", got)
	}

	ownershipCall(t, pod, instance, "round_trip", nil, nil)
	if got := head(); got != (ownershipFound{true, 5}) || instance.Live() != 5*ownershipBlock {
		t.Errorf("taking the chain out and back frees nothing: head %+v, %d bytes live", got, instance.Live())
	}

	var popped ownershipFound
	ownershipCall(t, pod, instance, "drop_head", nil, unsafe.Pointer(&popped))
	if popped != (ownershipFound{true, 5}) || instance.Live() != 4*ownershipBlock {
		t.Errorf("drop_head = %+v, %d bytes live", popped, instance.Live())
	}

	// Each link owns the next, so replacing the head frees the four that were left at once.
	value := int64(9)
	ownershipCall(t, pod, instance, "replace_chain", unsafe.Pointer(&value), nil)
	if length() != 1 || instance.Live() != ownershipBlock {
		t.Errorf("assigning the place frees the links that were there: length %d, %d bytes live", length(), instance.Live())
	}

	ownershipCall(t, pod, instance, "clear_chain", nil, nil)
	if got := head(); got != (ownershipFound{}) || length() != 0 || instance.Live() != 0 {
		t.Errorf("after clear_chain: head %+v, length %d, %d bytes live", got, length(), instance.Live())
	}

	ownershipCall(t, pod, instance, "drop_head", nil, unsafe.Pointer(&popped))
	if popped != (ownershipFound{}) {
		t.Errorf("drop_head on an empty chain = %+v", popped)
	}
}

// A struct with an own field owns it in turn: it frees its fields when the function ends, hands
// them over when it is moved into a place, and frees what a field held when that field is
// assigned.
func TestOwnershipOwningStruct(t *testing.T) {
	pod, instance := ownershipPod(t, 1<<10)
	corners := ownershipCorners{3, 4}

	for _, name := range []string{"pair_sum", "replace_first"} {
		var sum int64
		ownershipCall(t, pod, instance, name, unsafe.Pointer(&corners), unsafe.Pointer(&sum))

		if sum != corners.X+corners.Y || instance.Live() != 0 {
			t.Errorf("%s = %d, %d bytes live", name, sum, instance.Live())
		}
	}

	ownershipCall(t, pod, instance, "park_pair", unsafe.Pointer(&corners), nil)
	if live := instance.Live(); live != 3*ownershipBlock {
		t.Errorf("a pair and the two points it holds take three blocks, %d bytes live", live)
	}

	ownershipCall(t, pod, instance, "clear_pair", nil, nil)
	if live := instance.Live(); live != 0 {
		t.Errorf("after clear_pair: %d bytes live", live)
	}
}

// A function may answer with an own that holds nothing, which the caller binds like any other.
func TestOwnershipOptionalAnswer(t *testing.T) {
	pod, instance := ownershipPod(t, ownershipBlock)

	run := func(nothing bool) ownershipFound {
		t.Helper()

		args := struct {
			X, Y    int64
			Nothing bool
		}{3, 4, nothing}

		var got ownershipFound
		ownershipCall(t, pod, instance, "maybe_point", unsafe.Pointer(&args), unsafe.Pointer(&got))
		return got
	}

	if got := run(false); got != (ownershipFound{true, 7}) {
		t.Errorf("maybe_point = %+v", got)
	}
	if got := run(true); got != (ownershipFound{}) {
		t.Errorf("maybe_point with nothing to answer = %+v", got)
	}
	if live := instance.Live(); live != 0 {
		t.Errorf("%d bytes live", live)
	}
}

// A value read as an argument is not handed over until the call happens: an argument after it
// that faults leaves it with the name that held it, and the way out frees it like any other.
// The heap holds one block, so the call that follows proves the block came back.
func TestOwnershipFaultBeforeTheMove(t *testing.T) {
	pod, instance := ownershipPod(t, ownershipBlock)
	lost := harness.Function(t, pod, "lost_on_a_fault")

	divisor := int64(2)
	if _, err := instance.Call(lost, unsafe.Pointer(&divisor), nil, 0); err != nil {
		t.Fatal(err)
	}

	divisor = 0
	gasLeft, err := instance.Call(lost, unsafe.Pointer(&divisor), nil, 0)

	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Name != "division by zero" || failure.Code != -3 {
		t.Fatalf("dividing by zero should fault, got %v", err)
	}
	if gasLeft != failure.Code {
		t.Errorf("the gas a fault returns is its code, got %d", gasLeft)
	}

	want := bluecode.Frame{Function: "lost_on_a_fault", Line: 202, Raised: "division by zero"}
	if len(failure.Trace) != 1 || failure.Trace[0] != want {
		t.Errorf("trace = %+v", failure.Trace)
	}
	if live := instance.Live(); live != 0 {
		t.Errorf("the value read before the fault is freed all the same, %d bytes live", live)
	}

	divisor = 2
	if _, err := instance.Call(lost, unsafe.Pointer(&divisor), nil, 0); err != nil {
		t.Fatalf("the block should have gone back to the heap: %v", err)
	}
}
