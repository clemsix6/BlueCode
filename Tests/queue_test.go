package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// Links are taken out of the state and freed as they go: the heap is empty once the queue is.
func TestQueue(t *testing.T) {
	pod := harness.Build(t, "programs/queue.bc")
	instance, err := pod.NewInstance(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	enqueue := harness.Function(t, pod, "enqueue")
	dequeue := harness.Function(t, pod, "dequeue")
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
	instance.Call(harness.Function(t, pod, "pending"), nil, unsafe.Pointer(&pending), 0)
	if pending != 2 {
		t.Errorf("pending = %d", pending)
	}

	instance.Call(harness.Function(t, pod, "clear"), nil, nil, 0)
	instance.Call(dequeue, nil, unsafe.Pointer(&got), 0)
	if got.Ok || instance.Live() != 0 {
		t.Errorf("after clear: dequeue = %+v, live %d", got, instance.Live())
	}

	// Two instances of one pod know nothing of each other.
	other, _ := pod.NewInstance(1 << 16)
	defer other.Close()
	args := transfer{9, 9, 9}
	other.Call(enqueue, unsafe.Pointer(&args), nil, 0)
	instance.Call(harness.Function(t, pod, "pending"), nil, unsafe.Pointer(&pending), 0)
	other.Call(harness.Function(t, pod, "pending"), nil, unsafe.Pointer(&args), 0)
	if pending != 0 || args.From != 1 {
		t.Errorf("instances share state: %d and %d pending", pending, args.From)
	}
}
