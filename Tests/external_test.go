package tests

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// externalGas is the budget these calls are given; none of them spends any of it.
const externalGas = 1_000_000

// externalLedger mirrors the Ledger struct of external.bc.
type externalLedger struct {
	Total  uint64 // Total is what has been recorded so far, fees deducted
	Count  uint64 // Count is how many amounts were recorded
	Closed bool   // Closed tells whether the ledger takes anything more
}

// externalRecord mirrors the arguments of record: a value, a ref, and a value again, so the
// ref has to sit inline between them at the full size of the struct.
type externalRecord struct {
	Before uint64         // Before is a number the pod only reads
	Ledger externalLedger // Ledger is the block the pod writes back into
	Amount uint64         // Amount is what to record in it
}

// externalSplit mirrors the arguments of split.
type externalSplit struct {
	Ledger externalLedger // Ledger is the ledger the amount is taken from
	Amount uint64         // Amount is what to split into a net part and a fee
}

// externalSplitResults mirrors the results of split: its two values, then the error, which a
// function that can fail always carries in the last field.
type externalSplitResults struct {
	Net   uint64 // Net is the amount left once the fee is taken
	Fee   uint64 // Fee is what the ledger kept
	Error int64  // Error is the number of the error, zero when the call succeeded
}

// externalUsable mirrors the arguments of usable.
type externalUsable struct {
	Ledger       externalLedger // Ledger is the ledger to judge
	RequireEmpty bool           // RequireEmpty demands that nothing was recorded yet
}

// TestArgumentBlocks checks the block a host fills before a call: parameters in order, a ref
// among them taking the room of the whole struct rather than of a pointer.
func TestArgumentBlocks(t *testing.T) {
	pod := harness.Build(t, "programs/external.bc")
	record := harness.Function(t, pod, "record")

	if uintptr(record.Args.Size) != unsafe.Sizeof(externalRecord{}) || record.Args.Size != 40 {
		t.Fatalf("record takes %d bytes, the Go mirror is %d", record.Args.Size, unsafe.Sizeof(externalRecord{}))
	}

	fields := record.Args.Fields
	if fields[1].Name != "ledger" || !fields[1].Ref || fields[1].Type != "Ledger" || fields[1].Offset != 8 {
		t.Errorf("the ref should sit inline at the offset Go gives it: %+v", fields[1])
	}
	if fields[0].Ref || fields[2].Ref || fields[2].Offset != 32 {
		t.Errorf("only the ref parameter is marked, and the value after it follows the whole struct: %+v", fields)
	}

	if nothing := harness.Function(t, pod, "nothing"); nothing.Args.Size != 0 || len(nothing.Args.Fields) != 0 {
		t.Errorf("a function with no parameters has no argument block: %+v", nothing.Args)
	}
}

// TestResultBlocks checks the block a host reads after a call: the values in order, then the
// error when the function can fail, and nothing at all when it returns nothing.
func TestResultBlocks(t *testing.T) {
	pod := harness.Build(t, "programs/external.bc")

	split := harness.Function(t, pod, "split")
	if uintptr(split.Results.Size) != unsafe.Sizeof(externalSplitResults{}) {
		t.Fatalf("split gives back %d bytes, the Go mirror is %d", split.Results.Size, unsafe.Sizeof(externalSplitResults{}))
	}
	last := split.Results.Fields[len(split.Results.Fields)-1]
	if last.Type != "error" || last.Offset != 16 || last.Name != "" {
		t.Errorf("the error is the last field of the results and has no name: %+v", split.Results.Fields)
	}

	check := harness.Function(t, pod, "check")
	if check.Results.Size != 8 || len(check.Results.Fields) != 1 || check.Results.Fields[0].Type != "error" {
		t.Errorf("a failing function with no value gives back its error alone: %+v", check.Results)
	}

	if closer := harness.Function(t, pod, "close"); closer.Results.Size != 0 {
		t.Errorf("a function that returns nothing and cannot fail has no result block: %+v", closer.Results)
	}
}

// TestManifestHeader reads what the manifest says about the pod as a whole: the file it came
// from, the depth its code enforces, its errors numbered from one, its structs and its state.
func TestManifestHeader(t *testing.T) {
	manifest := harness.Build(t, "programs/external.bc").Manifest()

	if manifest.Source != "external.bc" || manifest.MaxDepth != 100_000 {
		t.Errorf("source %q, max depth %d", manifest.Source, manifest.MaxDepth)
	}

	want := []bluecode.ErrorName{{Name: "Closed", Code: 1}, {Name: "Overdrawn", Code: 2}}
	if !slices.Equal(manifest.Errors, want) {
		t.Errorf("errors are numbered from one in declaration order: %+v", manifest.Errors)
	}

	if len(manifest.Structs) != 1 || manifest.Structs["Ledger"].Size != 24 {
		t.Errorf("every declared struct is in the manifest: %+v", manifest.Structs)
	}

	if manifest.State.Size != 0 || len(manifest.State.Fields) != 0 {
		t.Errorf("a pod with no state variable has an empty state block: %+v", manifest.State)
	}
}

// TestManifestFunctions reads the function list: file order, which is the numbering a trace
// refers to, with an internal function carrying nothing but its name.
func TestManifestFunctions(t *testing.T) {
	manifest := harness.Build(t, "programs/external.bc").Manifest()

	order := []string{"fee", "record", "close", "nothing", "split", "check", "usable"}
	got := make([]string, len(manifest.Functions))
	for i, function := range manifest.Functions {
		got[i] = function.Name
	}
	if !slices.Equal(got, order) {
		t.Fatalf("functions are listed in file order: %v", got)
	}

	if fee := manifest.Functions[0]; fee.External || fee.Symbol != "" || fee.Fails || fee.Args.Size != 0 {
		t.Errorf("an internal function is listed by name only: %+v", fee)
	}

	for _, function := range manifest.Functions[1:] {
		fails := function.Name == "split" || function.Name == "check"
		if !function.External || function.Symbol != "bc_"+function.Name || function.Fails != fails {
			t.Errorf("external function %+v", function)
		}
	}
}

// TestBoundaryValues calls across the boundary: a ref the pod writes back into, blocks of no
// bytes passed as nothing, and plain values coming home.
func TestBoundaryValues(t *testing.T) {
	pod := harness.Build(t, "programs/external.bc")

	args, count := externalRecord{Before: 5, Amount: 1000}, uint64(0)
	if _, err := harness.Function(t, pod, "record").Call(unsafe.Pointer(&args), unsafe.Pointer(&count), externalGas); err != nil {
		t.Fatal(err)
	}
	if args.Ledger != (externalLedger{Total: 990, Count: 1}) || count != 6 {
		t.Errorf("record left %+v and returned %d", args.Ledger, count)
	}

	if _, err := harness.Function(t, pod, "nothing").Call(nil, nil, externalGas); err != nil {
		t.Errorf("a call with neither arguments nor results: %v", err)
	}

	if _, err := harness.Function(t, pod, "close").Call(unsafe.Pointer(&args.Ledger), nil, externalGas); err != nil || !args.Ledger.Closed {
		t.Errorf("close left %+v, %v", args.Ledger, err)
	}

	judged, usable := externalUsable{Ledger: externalLedger{Count: 0}, RequireEmpty: true}, false
	harness.Function(t, pod, "usable").Call(unsafe.Pointer(&judged), unsafe.Pointer(&usable), externalGas)
	if !usable {
		t.Error("an open and empty ledger is usable")
	}
}

// TestBoundaryFailures reads the error a failing call leaves in the last field of its results,
// and the failure the host gets with it.
func TestBoundaryFailures(t *testing.T) {
	pod := harness.Build(t, "programs/external.bc")
	split := harness.Function(t, pod, "split")

	args := externalSplit{externalLedger{Total: 990}, 500}
	var results externalSplitResults
	gasLeft, err := split.Call(unsafe.Pointer(&args), unsafe.Pointer(&results), externalGas)
	if err != nil || results != (externalSplitResults{Net: 495, Fee: 5}) || gasLeft != externalGas {
		t.Errorf("split(990, 500) = %+v with %d gas left, %v", results, gasLeft, err)
	}

	args.Amount = 2000
	results = externalSplitResults{Net: 9, Fee: 9}
	_, err = split.Call(unsafe.Pointer(&args), unsafe.Pointer(&results), externalGas)
	externalFailed(t, err, "Overdrawn", 2)
	if results.Net != 0 || results.Fee != 0 || results.Error != 2 {
		t.Errorf("a failure zeroes the values and names itself in the last field: %+v", results)
	}

	closed := externalLedger{Closed: true}
	var only int64
	_, err = harness.Function(t, pod, "check").Call(unsafe.Pointer(&closed), unsafe.Pointer(&only), externalGas)
	externalFailed(t, err, "Closed", 1)
	if only != 1 {
		t.Errorf("the error alone is the whole result block: %d", only)
	}
}

// externalFailed checks that a call failed with one of the pod's own errors, by name and by
// the number the manifest gives it.
func externalFailed(t *testing.T, err error, name string, code int64) {
	t.Helper()

	var failure *bluecode.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("expected the %s error, got %v", name, err)
	}

	if failure.Name != name || failure.Code != code {
		t.Errorf("failed with %s (%d), expected %s (%d)", failure.Name, failure.Code, name, code)
	}
}

// TestLoaderRejections feeds the loader what it must refuse: a pod for another CPU, a manifest
// promising entries the object does not have, and names that are not entries at all.
func TestLoaderRejections(t *testing.T) {
	artifacts := harness.Compile(t, "programs/external.bc")

	if _, err := bluecode.Load(artifacts.Foreign, artifacts.Manifest); err == nil || !strings.Contains(err.Error(), "built for") {
		t.Errorf("an object built for the other CPU should be refused: %v", err)
	}

	other := harness.Compile(t, "programs/functions.bc")
	if _, err := bluecode.Load(artifacts.Native, other.Manifest); err == nil || !strings.Contains(err.Error(), "has no bc_") {
		t.Errorf("a manifest naming entries the object has not should be refused: %v", err)
	}

	pod := harness.Build(t, "programs/external.bc")
	if _, err := pod.Function("fee"); err == nil || !strings.Contains(err.Error(), "not external") {
		t.Errorf("an internal function is no entry: %v", err)
	}
	if _, err := pod.Function("nowhere"); err == nil || !strings.Contains(err.Error(), "no function") {
		t.Errorf("a name the pod does not know: %v", err)
	}
}
