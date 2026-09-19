package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// Mirrors of the structs in geometry.bc.
type Point struct{ X, Y int64 }

type Line struct{ Start, End Point }

func TestStructsAndMultipleResults(t *testing.T) {
	pod := harness.Build(t, "programs/geometry.bc")

	line := Line{Point{1, 2}, Point{4, 6}}
	var length uint64
	harness.Function(t, pod, "size").Call(unsafe.Pointer(&line), unsafe.Pointer(&length), 0)
	if length != 7 {
		t.Errorf("size = %d", length)
	}

	operands := struct{ X, Y int64 }{10, 3}
	var results struct{ Sum, Difference int64 }
	harness.Function(t, pod, "operate").Call(unsafe.Pointer(&operands), unsafe.Pointer(&results), 0)
	if results.Sum != 13 || results.Difference != 7 {
		t.Errorf("operate(10, 3) = %+v", results)
	}

	x, absolute := int64(-7), uint64(0)
	harness.Function(t, pod, "abs").Call(unsafe.Pointer(&x), unsafe.Pointer(&absolute), 0)
	if absolute != 7 {
		t.Errorf("abs(-7) = %d", absolute)
	}

	// A struct built by the pod comes back laid out like the one Go builds.
	corners := struct{ X1, Y1, X2, Y2 int64 }{1, 2, 4, 6}
	var built Line
	harness.Function(t, pod, "segment").Call(unsafe.Pointer(&corners), unsafe.Pointer(&built), 0)
	if built != line {
		t.Errorf("segment = %+v", built)
	}
}
