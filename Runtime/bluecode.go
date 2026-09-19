// Package bluecode loads pods compiled from BlueCode and calls their functions.
//
// A pod is an ELF object holding position-independent machine code and nothing else, with a
// JSON manifest describing the memory layout of every function's arguments and results.
// Loading copies the code into executable memory. Calling a function jumps straight into it,
// on a stack sized for the pod, with no runtime in between: a few nanoseconds.
//
// Arguments and results are plain memory. A Go struct with the same fields as the BlueCode
// declaration, in the same order, has the same layout and is passed by address:
//
//	transfer, _ := pod.Function("transfer")
//	args := struct{ From, To Account; Amount uint64 }{from, to, 1000}
//	var results struct{ Left uint64; Err int64 }
//	gasLeft, err := transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&results), 1_000_000)
//
// A function that can fail ends its results with the number of its error, and Call turns that,
// or a fault such as running too deep, into a *Failure carrying the trace the pod recorded.
//
// A pod may keep state between calls and own memory of its own; both live in an Instance the
// host creates, and never cross to the host: only external functions can be called, and they
// take and return plain values. A plain Call runs on a throwaway instance instead.
package bluecode

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Pod is a loaded pod. Its functions may be called from any number of goroutines at once.
type Pod struct {
	code      []byte // the executable copy of the object's .text
	manifest  *Manifest
	functions map[string]*Function
	stacks    *stackPool
	stackSize int
	layout    instanceLayout // of the instance a plain Call runs on
}

// Function is one entry of a pod, ready to call.
type Function struct {
	Name    string
	Args    Block // layout of the block Call reads the arguments from
	Results Block // layout of the block Call writes the results to
	entry   uintptr
	pod     *Pod
	fails   bool // whether the results end with an error field
	errorAt int  // its offset in the results block
}

// Load reads a pod from its object file and its manifest.
func Load(objectPath, manifestPath string) (*Pod, error) {
	object, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, err
	}

	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	return LoadBytes(object, manifest)
}

// LoadBytes loads a pod from the contents of its object file and its manifest.
func LoadBytes(object, manifest []byte) (*Pod, error) {
	parsed, err := parseObject(object)
	if err != nil {
		return nil, err
	}

	layouts, err := parseManifest(manifest)
	if err != nil {
		return nil, err
	}

	code, err := mapExecutable(parsed.text)
	if err != nil {
		return nil, err
	}

	size := stackSize(layouts.MaxDepth, parsed.maxFrame)
	layout := instanceLayout{state: layouts.State.Size, heap: DefaultHeapSize}
	pod := &Pod{code, layouts, make(map[string]*Function), newStackPool(size, traceSize(layouts.MaxDepth), layout), size, layout}
	base := addressOf(code)

	for _, signature := range layouts.Functions {
		if !signature.External {
			continue
		}

		offset, ok := parsed.entries[signature.Symbol]
		if !ok {
			pod.Close()
			return nil, fmt.Errorf("pod: manifest lists %s but the object has no %s", signature.Name, signature.Symbol)
		}

		function := &Function{
			Name:    signature.Name,
			Args:    signature.Args,
			Results: signature.Results,
			entry:   base + uintptr(offset),
			pod:     pod,
			fails:   signature.Fails,
		}
		if signature.Fails {
			function.errorAt = signature.Results.Fields[len(signature.Results.Fields)-1].Offset
		}
		pod.functions[signature.Name] = function
	}

	return pod, nil
}

// Function returns the function of that name, which must be external: an internal function
// has no entry the host could reach.
func (p *Pod) Function(name string) (*Function, error) {
	function, ok := p.functions[name]
	if !ok {
		for _, signature := range p.manifest.Functions {
			if signature.Name == name {
				return nil, fmt.Errorf("pod: %s is not external", name)
			}
		}
		return nil, fmt.Errorf("pod: no function %q", name)
	}

	return function, nil
}

// Manifest returns the layouts the pod was loaded with.
func (p *Pod) Manifest() *Manifest {
	return p.manifest
}

// StackSize is the size of the stack every call of this pod runs on: what its deepest
// possible call chain needs, computed from the manifest's depth limit and the largest frame
// recorded in the object.
func (p *Pod) StackSize() int {
	return p.stackSize
}

// Close releases the code. The pod's functions must not be called afterwards.
func (p *Pod) Close() error {
	if p.code == nil {
		return nil
	}

	err := syscall.Munmap(p.code)
	p.code = nil
	p.functions = nil
	return err
}

// Call runs the function on an instance of its own, wiped beforehand: the pod's state starts
// out zero and whatever it allocates is gone afterwards. Instance.Call keeps both between calls.
//
// args points at a block laid out as f.Args says, results at one laid out as f.Results says;
// both must be Go memory. gas is the budget; what is left comes back. The error is a *Failure
// when the function failed with one of the pod's errors, or when the pod gave up: the gas is
// then negative and the results are all zero. Either way, what the function wrote through its
// ref arguments before failing stays written.
func (f *Function) Call(args, results unsafe.Pointer, gas int64) (int64, error) {
	stack := f.pod.stacks.get()
	if !f.pod.layout.untouched(stack.instance) {
		f.pod.layout.reset(stack.instance)
	}
	left, err := f.call(stack, addressOf(stack.instance), args, results, gas)
	f.pod.stacks.put(stack)
	return left, err
}

func (f *Function) call(stack *stack, instance uintptr, args, results unsafe.Pointer, gas int64) (int64, error) {
	left := call(f.entry, uintptr(args), uintptr(results), addressOf(stack.trace), instance, uintptr(gas), stack.top)

	var err error
	if left < 0 {
		err = f.pod.failure(stack.trace, left)
	} else if f.fails {
		if code := *(*int64)(unsafe.Add(results, f.errorAt)); code != 0 {
			err = f.pod.failure(stack.trace, code)
		}
	}

	return left, err
}
