package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// Mirrors of the structs of refs.bc: the same fields in the same order lay out identically, so
// a Go value is the argument block itself and a ref parameter is written back in place.
type refsPoint struct{ X, Y int64 }

type refsLine struct{ Start, End refsPoint }

type refsBox struct {
	Edge   refsLine
	Weight int64
}

// A ref parameter sits in the argument block at full size and the pod writes into it, so the
// host reads back what the function left there.
func TestRefsWriteThroughAParameter(t *testing.T) {
	pod := harness.Build(t, "programs/refs.bc")

	t.Run("the whole place", func(t *testing.T) {
		args := struct{ A, B int64 }{1, 2}
		harness.Function(t, pod, "swap").Call(unsafe.Pointer(&args), nil, 0)

		if args.A != 2 || args.B != 1 {
			t.Errorf("swap left %+v", args)
		}
	})

	t.Run("a field handed on", func(t *testing.T) {
		args := struct {
			Line   refsLine
			Dx, Dy int64
		}{refsLine{refsPoint{-3, 5}, refsPoint{4, -6}}, 10, 10}
		harness.Function(t, pod, "shift").Call(unsafe.Pointer(&args), nil, 0)

		if want := (refsLine{refsPoint{7, 15}, refsPoint{14, 4}}); args.Line != want {
			t.Errorf("shift left %+v, want %+v", args.Line, want)
		}
	})

	t.Run("two levels of fields", func(t *testing.T) {
		args := struct {
			Box    refsBox
			Dx, Dy int64
		}{refsBox{refsLine{refsPoint{1, 2}, refsPoint{3, 4}}, 7}, 10, 20}
		harness.Function(t, pod, "shift_edge").Call(unsafe.Pointer(&args), nil, 0)

		if want := (refsBox{refsLine{refsPoint{11, 22}, refsPoint{3, 4}}, 8}); args.Box != want {
			t.Errorf("shift_edge left %+v, want %+v", args.Box, want)
		}
	})

	t.Run("a value parameter is a copy", func(t *testing.T) {
		args := struct {
			Point refsPoint
			Dx    int64
		}{refsPoint{1, 1}, 5}
		var x int64
		harness.Function(t, pod, "moved_x").Call(unsafe.Pointer(&args), unsafe.Pointer(&x), 0)

		if x != 6 || args.Point != (refsPoint{1, 1}) {
			t.Errorf("moved_x = %d and left %+v", x, args.Point)
		}
	})
}

// A ref variable names one place for its block, whether it was bound to a variable or to a
// field, and reading it hands out a value of its own.
func TestRefsVariables(t *testing.T) {
	pod := harness.Build(t, "programs/refs.bc")

	t.Run("bound to a local", func(t *testing.T) {
		start, got := int64(5), int64(0)
		harness.Function(t, pod, "bumped").Call(unsafe.Pointer(&start), unsafe.Pointer(&got), 0)

		if got != 7 {
			t.Errorf("bumped(5) = %d", got)
		}
	})

	t.Run("bound to a field", func(t *testing.T) {
		args := struct {
			Line  refsLine
			Floor int64
		}{refsLine{refsPoint{-3, -5}, refsPoint{4, 6}}, 0}
		harness.Function(t, pod, "clamp_start").Call(unsafe.Pointer(&args), nil, 0)

		if want := (refsLine{refsPoint{0, 0}, refsPoint{4, 6}}); args.Line != want {
			t.Errorf("clamp_start left %+v, want %+v", args.Line, want)
		}
	})

	t.Run("bound two levels down", func(t *testing.T) {
		args := struct {
			Box refsBox
			By  int64
		}{refsBox{refsLine{refsPoint{1, 2}, refsPoint{3, 4}}, 0}, 10}
		harness.Function(t, pod, "widen").Call(unsafe.Pointer(&args), nil, 0)

		if want := (refsBox{refsLine{refsPoint{1, 2}, refsPoint{13, 4}}, 0}); args.Box != want {
			t.Errorf("widen left %+v, want %+v", args.Box, want)
		}
	})

	t.Run("a copy taken from a ref", func(t *testing.T) {
		n := int64(1)
		var results struct{ Copy, Place int64 }
		harness.Function(t, pod, "copy_then_write").Call(unsafe.Pointer(&n), unsafe.Pointer(&results), 0)

		if results.Copy != 101 || results.Place != 2 || n != 2 {
			t.Errorf("copy_then_write = %+v with the place left at %d", results, n)
		}
	})
}

// What the freeze allows: a sibling of the frozen place is writable while the ref lives, and
// the place itself is writable again once the block that declared the ref is over.
func TestRefsFreeze(t *testing.T) {
	pod := harness.Build(t, "programs/refs.bc")

	t.Run("a sibling stays writable", func(t *testing.T) {
		args := struct {
			Line refsLine
			By   int64
		}{refsLine{refsPoint{1, 2}, refsPoint{0, 0}}, 10}
		harness.Function(t, pod, "raise_end").Call(unsafe.Pointer(&args), nil, 0)

		if want := (refsLine{refsPoint{1, 2}, refsPoint{11, 12}}); args.Line != want {
			t.Errorf("raise_end left %+v, want %+v", args.Line, want)
		}
	})

	t.Run("the place once the block is over", func(t *testing.T) {
		n, got := int64(3), int64(0)
		entry := harness.Function(t, pod, "frozen_until_the_block_ends")
		entry.Call(unsafe.Pointer(&n), unsafe.Pointer(&got), 0)

		if got != 8 {
			t.Errorf("frozen_until_the_block_ends(3) = %d", got)
		}
	})
}

// The mirrors above only hold while they lay out as the manifest says, and a ref parameter is
// what the manifest marks as one.
func TestRefsLayout(t *testing.T) {
	pod := harness.Build(t, "programs/refs.bc")
	structs := pod.Manifest().Structs

	sizes := map[string]uintptr{
		"Point": unsafe.Sizeof(refsPoint{}),
		"Line":  unsafe.Sizeof(refsLine{}),
		"Box":   unsafe.Sizeof(refsBox{}),
	}

	for name, size := range sizes {
		if got := uintptr(structs[name].Size); got != size {
			t.Errorf("%s is %d bytes in the manifest, %d in Go", name, got, size)
		}
	}

	for _, field := range harness.Function(t, pod, "swap").Args.Fields {
		if !field.Ref {
			t.Errorf("argument %q of swap is not marked as a ref", field.Name)
		}
	}
}
