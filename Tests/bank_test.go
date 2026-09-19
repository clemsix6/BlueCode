package tests

import (
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// Mirrors of the structs in bank.bc.
type Account struct {
	Id      uint64
	Balance uint64
	Frozen  bool
}

type Fees struct {
	Flat        uint64
	PerThousand uint64
}

type transferArgs struct {
	From, To Account // taken by ref: the pod updates them in place
	Amount   uint64
	Fees     Fees
}

func TestTransfer(t *testing.T) {
	pod := harness.Build(t, "programs/bank.bc")
	transfer := harness.Function(t, pod, "transfer")

	if unsafe.Sizeof(transferArgs{}) != uintptr(transfer.Args.Size) || transfer.Results.Size != 1 {
		t.Fatalf("Go structs do not match the manifest: args %d, results %d", transfer.Args.Size, transfer.Results.Size)
	}
	if !transfer.Args.Fields[0].Ref || !transfer.Args.Fields[1].Ref || transfer.Args.Fields[2].Ref {
		t.Errorf("the manifest should mark from and to as ref: %+v", transfer.Args.Fields)
	}

	args := transferArgs{Account{1, 5000, false}, Account{2, 100, false}, 1000, Fees{10, 5}}
	var ok bool

	gasLeft, err := transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&ok), 1_000_000)
	if !ok || args.From.Balance != 3985 || args.To.Balance != 1100 || gasLeft != 1_000_000 || err != nil {
		t.Errorf("transfer = %v, accounts %+v, gas left %d, %v", ok, args, gasLeft, err)
	}

	args.From.Frozen = true
	transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&ok), 1_000_000)
	if ok || args.From.Balance != 3985 || args.To.Balance != 1100 {
		t.Errorf("a transfer from a frozen account should change nothing: %v, %+v", ok, args)
	}
}

func TestRejects(t *testing.T) {
	artifacts := harness.Compile(t, "programs/bank.bc")
	if _, err := bluecode.Load(artifacts.Foreign, artifacts.Manifest); err == nil {
		t.Error("an object for another CPU should be rejected")
	}

	pod := harness.Build(t, "programs/bank.bc")
	if _, err := pod.Function("nothing"); err == nil {
		t.Error("an unknown function should be rejected")
	}
	if _, err := pod.Function("fee_for"); err == nil {
		t.Error("an internal function should be rejected")
	}
}
