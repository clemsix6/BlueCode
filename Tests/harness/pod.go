package harness

import (
	"testing"

	"bluecode/runtime"
)

// Build compiles a program and loads the pod built for this CPU. The pod closes with the test.
func Build(t testing.TB, program string) *bluecode.Pod {
	t.Helper()

	artifacts := Compile(t, program)

	pod, err := bluecode.Load(artifacts.Native, artifacts.Manifest)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { pod.Close() })

	return pod
}

// Function returns one entry of a pod, failing the test when the pod does not export it.
func Function(t testing.TB, pod *bluecode.Pod, name string) *bluecode.Function {
	t.Helper()

	entry, err := pod.Function(name)
	if err != nil {
		t.Fatal(err)
	}

	return entry
}
