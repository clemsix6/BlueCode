package bluecode

import (
	"fmt"
	"strings"
	"unsafe"
)

// The trace a pod writes, as the compiler lays it out: the number of frames, then the frames,
// each holding the function's number, the line, and the error raised there, zero on a frame
// that only passed a failure on. The count keeps growing once the frames are full.
const (
	traceHeader = 8
	frameSize   = 24
)

// The faults a pod can give up with, by the negative gas it returns.
var faults = map[int64]string{
	-1: "too deep",
	-2: "out of gas",
	-3: "division by zero",
	-4: "overflow",
	-5: "out of memory",
}

// Failure is what a call returns when the function did not complete: an error the pod's code
// failed with, or a fault such as running too deep. Trace runs from the frame that raised the
// failure to the entry the host called; it is what the pod recorded, resolved through the manifest.
type Failure struct {
	Name   string  // the error's name, or the fault's
	Code   int64   // positive for an error the pod declares, negative for a fault
	Source string  // the file the pod was compiled from, which the lines of the frames refer to
	Trace  []Frame // from where the failure was raised to the entry the host called
	Lost   int     // frames the trace had no room for
}

// Frame is one function a failure went through.
type Frame struct {
	Function string
	Line     int
	Raised   string // the error raised on this frame, empty on one that only passed it on
}

// Error prints the failure and its trace. A catch block that fails with another error appends
// to the trace of the one it caught, so the trace can hold several errors: the last raised is
// the one returned, and each earlier one is printed as its cause.
func (f *Failure) Error() string {
	var text strings.Builder
	frames := f.Trace

	for len(frames) > 0 {
		start := lastRaised(frames)
		if text.Len() == 0 {
			text.WriteString(f.Name)
		} else {
			text.WriteString("\ncaused by " + frames[start].Raised)
		}
		for _, frame := range frames[start:] {
			fmt.Fprintf(&text, "\n    %s:%d %s", f.Source, frame.Line, frame.Function)
		}
		frames = frames[:start]
	}

	if text.Len() == 0 {
		text.WriteString(f.Name)
	}
	if f.Lost > 0 {
		fmt.Fprintf(&text, "\n    ... %d more", f.Lost)
	}

	return text.String()
}

// The index of the last frame that raised an error, where the error returned begins.
func lastRaised(frames []Frame) int {
	for i := len(frames) - 1; i > 0; i-- {
		if frames[i].Raised != "" {
			return i
		}
	}

	return 0
}

// failure reads the trace a call left and names what it holds.
func (p *Pod) failure(trace []byte, code int64) *Failure {
	count := int(*(*int64)(unsafe.Pointer(&trace[0])))
	capacity := p.manifest.MaxDepth + 1
	kept := min(count, capacity)

	frames := make([]Frame, kept)
	for i := range frames {
		raw := (*[3]int64)(unsafe.Pointer(&trace[traceHeader+i*frameSize]))
		frames[i] = Frame{Function: p.functionName(raw[0]), Line: int(raw[1]), Raised: p.errorName(raw[2])}
	}

	return &Failure{Name: p.errorName(code), Code: code, Source: p.manifest.Source, Trace: frames, Lost: count - kept}
}

func (p *Pod) functionName(number int64) string {
	if number >= 0 && int(number) < len(p.manifest.Functions) {
		return p.manifest.Functions[number].Name
	}

	return fmt.Sprintf("function %d", number)
}

func (p *Pod) errorName(code int64) string {
	if code == 0 {
		return ""
	}
	if name, ok := faults[code]; ok {
		return name
	}
	for _, e := range p.manifest.Errors {
		if e.Code == code {
			return e.Name
		}
	}

	return fmt.Sprintf("error %d", code)
}
