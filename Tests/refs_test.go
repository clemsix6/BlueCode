package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// A ref argument is read and written where it sits in the args block.
func TestRefs(t *testing.T) {
	pod := harness.Build(t, "programs/refs.bc")

	pair := struct{ A, B int64 }{1, 2}
	harness.Function(t, pod, "swap").Call(unsafe.Pointer(&pair), nil, 0)
	if pair.A != 2 || pair.B != 1 {
		t.Errorf("swap(1, 2) = %+v", pair)
	}

	shift := struct {
		Line   Line
		Dx, Dy int64
	}{Line{Point{-3, 5}, Point{4, -6}}, 10, 10}
	harness.Function(t, pod, "shift").Call(unsafe.Pointer(&shift), nil, 0)
	if shift.Line != (Line{Point{7, 15}, Point{14, 4}}) {
		t.Errorf("shift = %+v", shift.Line)
	}

	line := Line{Point{-3, -5}, Point{4, 6}}
	harness.Function(t, pod, "clamp_start").Call(unsafe.Pointer(&line), nil, 0)
	if line != (Line{Point{0, 0}, Point{4, 6}}) {
		t.Errorf("clamp_start = %+v", line)
	}

	points := struct{ A, B Point }{Point{9, 1}, Point{2, 3}}
	var span int64
	harness.Function(t, pod, "order").Call(unsafe.Pointer(&points), unsafe.Pointer(&span), 0)
	if span != 7 || points.A != (Point{2, 3}) || points.B != (Point{9, 1}) {
		t.Errorf("order = %d, %+v", span, points)
	}

	moved := struct {
		Point Point
		Dx    int64
	}{Point{1, 1}, 5}
	var x int64
	harness.Function(t, pod, "moved_x").Call(unsafe.Pointer(&moved), unsafe.Pointer(&x), 0)
	if x != 6 || moved.Point != (Point{1, 1}) {
		t.Errorf("moved_x should move a copy: %d, %+v", x, moved.Point)
	}
}
