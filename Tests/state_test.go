package tests

import (
	"errors"
	"slices"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// stateBlock is what one Point of the program takes on the heap.
const stateBlock = 16

// statePoint mirrors the program's Point: the same fields in the same order, so the host reads
// a result or writes an argument as its own memory.
type statePoint struct {
	X, Y int64
}

// stateHeld is what the pod answers when the point its state owns may be missing.
type stateHeld struct {
	Found bool
	X, Y  int64
}

// stateHolder is what the pod answers about the struct its state owns.
type stateHolder struct {
	Tag   uint64
	Found bool
	X     int64
}

// statePod loads the program with an instance of its own. The heap only has to hold a point or
// two: what this program pins is the state, not the allocator.
func statePod(t *testing.T) (*bluecode.Pod, *bluecode.Instance) {
	t.Helper()

	pod := harness.Build(t, "programs/state.bc")

	instance, err := pod.NewInstance(1 << 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })

	return pod, instance
}

// stateCall runs one function of the program on the instance and fails the test when the pod
// gives up or refuses.
func stateCall(t *testing.T, pod *bluecode.Pod, instance *bluecode.Instance, name string, args, results unsafe.Pointer) {
	t.Helper()

	if _, err := instance.Call(harness.Function(t, pod, name), args, results, 0); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// The state block is the contract for the memory the host hands the pod: the variables in
// declaration order, each with its name, its type and its offset, and the whole padded like a
// struct. Nothing in it that owns memory may be read as bytes.
func TestStateManifest(t *testing.T) {
	manifest := harness.Build(t, "programs/state.bc").Manifest()

	want := []bluecode.Field{
		{Name: "counter", Type: "uint", Offset: 0},
		{Name: "armed", Type: "bool", Offset: 8},
		{Name: "origin", Type: "Point", Offset: 16},
		{Name: "held", Type: "own Point?", Offset: 32},
		{Name: "holder", Type: "Holder", Offset: 40},
	}
	if !slices.Equal(manifest.State.Fields, want) {
		t.Errorf("state fields = %+v", manifest.State.Fields)
	}
	if manifest.State.Size != 56 {
		t.Errorf("state is %d bytes", manifest.State.Size)
	}

	if size := manifest.Structs["Point"].Size; int(unsafe.Sizeof(statePoint{})) != size {
		t.Errorf("Point is %d bytes in Go and %d in the pod", unsafe.Sizeof(statePoint{}), size)
	}

	if err := manifest.Canonical(manifest.State, make([]byte, manifest.State.Size)); err == nil {
		t.Error("a block holding an own should never be read as bytes")
	}
}

// What one call writes, the next call on the same instance finds — whether it is a number, a
// flag or a struct, and whether it was written by name or through a ref.
func TestStatePersists(t *testing.T) {
	pod, instance := statePod(t)

	by := uint64(7)
	for range 3 {
		stateCall(t, pod, instance, "bump", unsafe.Pointer(&by), nil)
	}

	var counter uint64
	stateCall(t, pod, instance, "count", nil, unsafe.Pointer(&counter))
	if counter != 3*by {
		t.Errorf("counter = %d", counter)
	}

	on := true
	stateCall(t, pod, instance, "arm", unsafe.Pointer(&on), nil)

	var armed bool
	stateCall(t, pod, instance, "is_armed", nil, unsafe.Pointer(&armed))
	if !armed {
		t.Error("the flag should have stayed set")
	}

	corners := statePoint{3, 4}
	stateCall(t, pod, instance, "move_origin", unsafe.Pointer(&corners), nil)

	shift := int64(10)
	stateCall(t, pod, instance, "shift_origin", unsafe.Pointer(&shift), nil)

	var origin statePoint
	stateCall(t, pod, instance, "origin_now", nil, unsafe.Pointer(&origin))
	if origin != (statePoint{13, 14}) {
		t.Errorf("a ref works on the state in place, origin = %+v", origin)
	}
}

// Two instances of one pod know nothing of each other, and a call made without an instance
// starts from a zeroed state every time.
func TestStateIsolation(t *testing.T) {
	pod, instance := statePod(t)

	other, err := pod.NewInstance(1 << 10)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	bump := harness.Function(t, pod, "bump")
	count := harness.Function(t, pod, "count")

	by := uint64(5)
	stateCall(t, pod, instance, "bump", unsafe.Pointer(&by), nil)
	stateCall(t, pod, instance, "bump", unsafe.Pointer(&by), nil)
	if _, err := other.Call(bump, unsafe.Pointer(&by), nil, 0); err != nil {
		t.Fatal(err)
	}

	var mine, theirs uint64
	stateCall(t, pod, instance, "count", nil, unsafe.Pointer(&mine))
	if _, err := other.Call(count, nil, unsafe.Pointer(&theirs), 0); err != nil {
		t.Fatal(err)
	}
	if mine != 2*by || theirs != by {
		t.Errorf("the instances share state: %d and %d", mine, theirs)
	}

	var plain uint64
	if _, err := bump.Call(unsafe.Pointer(&by), nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := count.Call(nil, unsafe.Pointer(&plain), 0); err != nil {
		t.Fatal(err)
	}
	if plain != 0 {
		t.Errorf("a call without an instance should keep nothing, got %d", plain)
	}
}

// The instance owns what its state holds and the pod never frees it on its own: only assigning
// the place does, or taking the value out of it.
func TestStateOwnValue(t *testing.T) {
	pod, instance := statePod(t)

	corners := statePoint{3, 4}
	stateCall(t, pod, instance, "keep", unsafe.Pointer(&corners), nil)
	if live := instance.Live(); live != stateBlock {
		t.Fatalf("the point the state holds outlives the call, %d bytes live", live)
	}

	var counter uint64
	stateCall(t, pod, instance, "count", nil, unsafe.Pointer(&counter))
	if live := instance.Live(); live != stateBlock {
		t.Errorf("a call that does not touch it should leave it alone, %d bytes live", live)
	}

	var held stateHeld
	stateCall(t, pod, instance, "held_now", nil, unsafe.Pointer(&held))
	if held != (stateHeld{true, 3, 4}) {
		t.Errorf("held_now = %+v", held)
	}

	corners = statePoint{5, 6}
	stateCall(t, pod, instance, "keep", unsafe.Pointer(&corners), nil)
	stateCall(t, pod, instance, "held_now", nil, unsafe.Pointer(&held))
	if held != (stateHeld{true, 5, 6}) || instance.Live() != stateBlock {
		t.Errorf("assigning the place frees what it held: %+v, %d bytes live", held, instance.Live())
	}

	var taken bool
	stateCall(t, pod, instance, "take_held", nil, unsafe.Pointer(&taken))
	stateCall(t, pod, instance, "held_now", nil, unsafe.Pointer(&held))
	if !taken || held != (stateHeld{}) || instance.Live() != 0 {
		t.Errorf("take leaves none behind: %v, %+v, %d bytes live", taken, held, instance.Live())
	}

	stateCall(t, pod, instance, "keep", unsafe.Pointer(&corners), nil)
	stateCall(t, pod, instance, "forget", nil, nil)
	if live := instance.Live(); live != 0 {
		t.Errorf("after forget: %d bytes live", live)
	}
}

// A struct that owns may be state too: the instance owns what its field holds, and assigning
// the field frees what was there.
func TestStateOwningStruct(t *testing.T) {
	pod, instance := statePod(t)

	var holder stateHolder
	stateCall(t, pod, instance, "holder_now", nil, unsafe.Pointer(&holder))
	if holder != (stateHolder{}) {
		t.Errorf("a fresh instance starts with an empty holder, got %+v", holder)
	}

	args := struct {
		Tag  uint64
		X, Y int64
	}{9, 3, 4}
	stateCall(t, pod, instance, "fill_holder", unsafe.Pointer(&args), nil)
	stateCall(t, pod, instance, "holder_now", nil, unsafe.Pointer(&holder))
	if holder != (stateHolder{9, true, 3}) || instance.Live() != stateBlock {
		t.Errorf("holder_now = %+v, %d bytes live", holder, instance.Live())
	}

	args.X = 5
	stateCall(t, pod, instance, "fill_holder", unsafe.Pointer(&args), nil)
	stateCall(t, pod, instance, "holder_now", nil, unsafe.Pointer(&holder))
	if holder != (stateHolder{9, true, 5}) || instance.Live() != stateBlock {
		t.Errorf("assigning the field frees the point it held: %+v, %d bytes live", holder, instance.Live())
	}

	stateCall(t, pod, instance, "empty_holder", nil, nil)
	stateCall(t, pod, instance, "holder_now", nil, unsafe.Pointer(&holder))
	if holder != (stateHolder{Tag: 9}) || instance.Live() != 0 {
		t.Errorf("after empty_holder: %+v, %d bytes live", holder, instance.Live())
	}
}

// A failure is not a rollback: what the call wrote to the state before giving up stays written.
func TestStateSurvivesAFailure(t *testing.T) {
	pod, instance := statePod(t)

	by := uint64(4)
	var raised int64
	_, err := instance.Call(harness.Function(t, pod, "bump_then_refuse"), unsafe.Pointer(&by), unsafe.Pointer(&raised), 0)

	var failure *bluecode.Failure
	if !errors.As(err, &failure) || failure.Name != "Refused" || failure.Code != 1 {
		t.Fatalf("bump_then_refuse should fail with Refused, got %v", err)
	}
	if raised != failure.Code {
		t.Errorf("the results end with the number of the error, got %d", raised)
	}

	var counter uint64
	stateCall(t, pod, instance, "count", nil, unsafe.Pointer(&counter))
	if counter != by {
		t.Errorf("what the call wrote before failing should have stayed, counter = %d", counter)
	}
}
