package tests

import (
	"testing"
	"unsafe"

	"bluecode/tests/harness"
)

// An instance keeps the pod's state and heap from one call to the next; nothing that owns
// memory ever crosses to the host, which only sees plain values.
func TestInstanceState(t *testing.T) {
	pod := harness.Build(t, "programs/ownership.bc")
	instance, err := pod.NewInstance(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	register := harness.Function(t, pod, "register")
	for _, account := range []struct{ Id, Balance uint64 }{{50, 500}, {20, 200}, {80, 800}, {10, 100}, {30, 300}, {20, 250}} {
		account := account
		if _, err := instance.Call(register, unsafe.Pointer(&account), nil, 0); err != nil {
			t.Fatal(err)
		}
	}

	var count uint64
	instance.Call(harness.Function(t, pod, "registered"), nil, unsafe.Pointer(&count), 0)
	instance.Call(harness.Function(t, pod, "nodes"), nil, unsafe.Pointer(&count), 0)
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
		instance.Call(harness.Function(t, pod, "balance"), unsafe.Pointer(&id), unsafe.Pointer(&result), 0)
		return result.Found, result.Balance
	}
	if found, balance := balanceOf(20); !found || balance != 250 {
		t.Errorf("balance(20) = %v, %d", found, balance)
	}
	if found, _ := balanceOf(60); found {
		t.Error("balance(60) should not be found")
	}

	// Pruning the left subtree of 50 frees 20, 10 and 30 at once.
	instance.Call(harness.Function(t, pod, "prune_left"), nil, nil, 0)
	instance.Call(harness.Function(t, pod, "nodes"), nil, unsafe.Pointer(&count), 0)
	if count != 2 || instance.Live() != 2*64 {
		t.Errorf("after prune_left: %d nodes, %d bytes live", count, instance.Live())
	}
	if found, _ := balanceOf(20); found {
		t.Error("balance(20) should be gone")
	}

	instance.Call(harness.Function(t, pod, "clear"), nil, nil, 0)
	instance.Call(harness.Function(t, pod, "nodes"), nil, unsafe.Pointer(&count), 0)
	if count != 0 || instance.Live() != 0 {
		t.Errorf("after clear: %d nodes, %d bytes live", count, instance.Live())
	}

	// A call without an instance starts from a zero state every time.
	if _, err := register.Call(unsafe.Pointer(&struct{ Id, Balance uint64 }{1, 1}), nil, 0); err != nil {
		t.Fatal(err)
	}
	harness.Function(t, pod, "registered").Call(nil, unsafe.Pointer(&count), 0)
	if count != 0 {
		t.Errorf("a plain call should not keep state, got %d registered", count)
	}
}
