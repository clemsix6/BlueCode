package tests

import (
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// structsGas is the budget these calls are given; none of them spends any of it.
const structsGas = 1_000_000

// structsFlags mirrors the Flags struct of structs.bc: two bools side by side.
type structsFlags struct {
	First  bool // First is the leading bool, at the very start of the struct
	Second bool // Second sits in the next byte, with no padding between them
}

// structsMixed mirrors the Mixed struct of structs.bc: a bool between eight-byte fields, so
// the bytes after each bool are padding the host owns.
type structsMixed struct {
	Flag  bool   // Flag takes one byte and leaves the rest of its slot to padding
	Value uint64 // Value starts the next eight-byte slot
	Tail  bool   // Tail opens a third slot, whose remainder pads the struct out
}

// structsNested mirrors the Nested struct of structs.bc: the widest member decides the
// alignment of the whole, so the two-byte one is followed by padding.
type structsNested struct {
	Flags structsFlags // Flags is two bytes wide and one-byte aligned
	Mixed structsMixed // Mixed is eight-byte aligned and pushes itself onto the next slot
}

// structsPoint mirrors the Point struct of structs.bc.
type structsPoint struct {
	X int64 // X is the first coordinate
	Y int64 // Y is the second
}

// structsLine mirrors the Line struct of structs.bc.
type structsLine struct {
	Start structsPoint // Start is where the segment begins
	End   structsPoint // End is where it stops
}

// structsStretch mirrors the arguments of stretch, whose line is taken by ref.
type structsStretch struct {
	Line structsLine // Line is the block the pod writes back into
	By   int64       // By is how far each coordinate of the end moves
}

// TestStructLayouts holds the manifest against the Go mirrors: same sizes, same offsets, so a
// Go struct can be handed to the pod as it is.
func TestStructLayouts(t *testing.T) {
	structs := harness.Build(t, "programs/structs.bc").Manifest().Structs

	if len(structs) != 6 {
		t.Errorf("every declared struct is in the manifest, crossing the boundary or not: %d", len(structs))
	}

	sizes := map[string]uintptr{
		"Flags":  unsafe.Sizeof(structsFlags{}),
		"Mixed":  unsafe.Sizeof(structsMixed{}),
		"Nested": unsafe.Sizeof(structsNested{}),
		"Point":  unsafe.Sizeof(structsPoint{}),
		"Line":   unsafe.Sizeof(structsLine{}),
	}
	for name, size := range sizes {
		if uintptr(structs[name].Size) != size {
			t.Errorf("%s is %d bytes in the manifest, %d in Go", name, structs[name].Size, size)
		}
	}

	if structs["Flags"].Size != 2 || structs["Mixed"].Size != 24 || structs["Nested"].Size != 32 {
		t.Errorf("sizes moved: Flags %d, Mixed %d, Nested %d", structs["Flags"].Size, structs["Mixed"].Size, structs["Nested"].Size)
	}
	structsOffsets(t, structs["Flags"], unsafe.Offsetof(structsFlags{}.Second))
	structsOffsets(t, structs["Mixed"], unsafe.Offsetof(structsMixed{}.Value), unsafe.Offsetof(structsMixed{}.Tail))
	structsOffsets(t, structs["Nested"], unsafe.Offsetof(structsNested{}.Mixed))
}

// structsOffsets checks the offsets of a block's fields after the first, which is always zero,
// against what Go computed for the mirror.
func structsOffsets(t *testing.T, block bluecode.Block, want ...uintptr) {
	t.Helper()

	if block.Fields[0].Offset != 0 {
		t.Errorf("the first field of a block starts at %d", block.Fields[0].Offset)
	}

	for i, offset := range want {
		if uintptr(block.Fields[i+1].Offset) != offset {
			t.Errorf("field %s is at %d in the manifest, %d in Go", block.Fields[i+1].Name, block.Fields[i+1].Offset, offset)
		}
	}
}

// TestTwoBoolsPack reads and writes both bools of a two-byte struct: the pod and the host
// agree on which byte is which.
func TestTwoBoolsPack(t *testing.T) {
	pod := harness.Build(t, "programs/structs.bc")

	for _, run := range []structsFlags{{false, false}, {true, false}, {false, true}, {true, true}} {
		args, and := run, false
		harness.Function(t, pod, "both").Call(unsafe.Pointer(&args), unsafe.Pointer(&and), structsGas)
		if and != (run.First && run.Second) {
			t.Errorf("both(%+v) = %v", run, and)
		}

		var flipped structsFlags
		harness.Function(t, pod, "flip").Call(unsafe.Pointer(&args), unsafe.Pointer(&flipped), structsGas)
		if flipped != (structsFlags{!run.First, !run.Second}) {
			t.Errorf("flip(%+v) = %+v", run, flipped)
		}
	}
}

// TestABoolBetweenWideFields passes a struct whose bool has an eight-byte neighbour on each
// side: the pod reads the one byte it owns and writes only that byte back.
func TestABoolBetweenWideFields(t *testing.T) {
	pod := harness.Build(t, "programs/structs.bc")

	mixed, weighed := structsMixed{true, 100, true}, uint64(0)
	if _, err := harness.Function(t, pod, "weigh").Call(unsafe.Pointer(&mixed), unsafe.Pointer(&weighed), structsGas); err != nil {
		t.Fatal(err)
	}
	if weighed != 103 {
		t.Errorf("weigh(%+v) = %d", mixed, weighed)
	}

	var rebuilt structsMixed
	harness.Function(t, pod, "rebuild").Call(unsafe.Pointer(&mixed), unsafe.Pointer(&rebuilt), structsGas)
	if rebuilt != mixed {
		t.Errorf("rebuild(%+v) = %+v", mixed, rebuilt)
	}
}

// TestNestedStructs reaches through a chain of dots, builds a struct from the inside out, and
// reads a field straight off one that was just built.
func TestNestedStructs(t *testing.T) {
	pod := harness.Build(t, "programs/structs.bc")
	nested := structsNested{structsFlags{false, true}, structsMixed{true, 7, false}}

	value, flag := uint64(0), false
	harness.Function(t, pod, "inner_value").Call(unsafe.Pointer(&nested), unsafe.Pointer(&value), structsGas)
	harness.Function(t, pod, "inner_flag").Call(unsafe.Pointer(&nested), unsafe.Pointer(&flag), structsGas)
	if value != 7 || !flag {
		t.Errorf("inner_value = %d, inner_flag = %v", value, flag)
	}

	var packed structsNested
	harness.Function(t, pod, "pack").Call(unsafe.Pointer(&nested), unsafe.Pointer(&packed), structsGas)
	if packed != nested {
		t.Errorf("pack(%+v) = %+v", nested, packed)
	}

	corners, y := struct{ X, Y int64 }{3, 4}, int64(0)
	harness.Function(t, pod, "corner").Call(unsafe.Pointer(&corners), unsafe.Pointer(&y), structsGas)
	if y != 4 {
		t.Errorf("corner(3, 4) = %d", y)
	}
}

// TestStructParametersAreCopies stretches a line by value and by ref: only the ref reaches
// back into the block the host handed over.
func TestStructParametersAreCopies(t *testing.T) {
	pod := harness.Build(t, "programs/structs.bc")

	args := structsStretch{structsLine{structsPoint{0, 0}, structsPoint{2, 2}}, 5}
	width := int64(0)
	harness.Function(t, pod, "stretched").Call(unsafe.Pointer(&args), unsafe.Pointer(&width), structsGas)
	if width != 7 || args.Line.End.X != 2 {
		t.Errorf("stretched = %d and left the caller's line %+v", width, args.Line)
	}

	if _, err := harness.Function(t, pod, "stretch").Call(unsafe.Pointer(&args), nil, structsGas); err != nil {
		t.Fatal(err)
	}
	if args.Line.End != (structsPoint{7, 7}) {
		t.Errorf("stretch left the caller's line at %+v", args.Line)
	}
}

// TestCanonicalBlocks runs the host's canonical check against the layouts this very pod was
// compiled with: one value, one encoding, so two nodes hash a transaction the same way.
func TestCanonicalBlocks(t *testing.T) {
	manifest := harness.Build(t, "programs/structs.bc").Manifest()
	mixed := manifest.Structs["Mixed"]

	canonical := make([]byte, mixed.Size)
	canonical[0], canonical[16] = 1, 1
	if err := manifest.Canonical(mixed, canonical); err != nil {
		t.Errorf("bools of one and padding of zero are canonical: %v", err)
	}

	notABool := make([]byte, mixed.Size)
	notABool[0] = 2
	structsRefused(t, manifest, mixed, notABool, "byte 0 is 2, not a bool")

	padded := make([]byte, mixed.Size)
	padded[1] = 1
	structsRefused(t, manifest, mixed, padded, "padding byte 1 is not zero")

	structsRefused(t, manifest, manifest.Structs["Flags"], []byte{1}, "block is 1 bytes, expected 2")
}

// TestCanonicalWalksNesting checks a block whose fields are structs of their own, and the one
// a host must never be handed at all: a struct that owns memory of the pod.
func TestCanonicalWalksNesting(t *testing.T) {
	manifest := harness.Build(t, "programs/structs.bc").Manifest()
	nested := manifest.Structs["Nested"]

	canonical := make([]byte, nested.Size)
	canonical[0], canonical[1], canonical[8], canonical[24] = 1, 1, 1, 1
	if err := manifest.Canonical(nested, canonical); err != nil {
		t.Errorf("a nested block of bools and zeros is canonical: %v", err)
	}

	inner := make([]byte, nested.Size)
	inner[8] = 3
	structsRefused(t, manifest, nested, inner, "byte 8 is 3, not a bool")

	held := manifest.Structs["Held"]
	if held.Size != 8 || held.Fields[0].Type != "own Point?" {
		t.Errorf("an own field is one pointer slot, named as it is written: %+v", held)
	}
	structsRefused(t, manifest, held, make([]byte, held.Size), `field "point" owns memory of the pod: it never crosses to the host`)
}

// structsRefused checks that the canonical form of a block rejects some bytes, and says so in
// the exact words the caller expects.
func structsRefused(t *testing.T, manifest *bluecode.Manifest, block bluecode.Block, data []byte, want string) {
	t.Helper()

	err := manifest.Canonical(block, data)
	if err == nil {
		t.Errorf("%q: the block was accepted", want)
		return
	}

	if err.Error() != want {
		t.Errorf("the block was refused with %q, expected %q", err, want)
	}
}
