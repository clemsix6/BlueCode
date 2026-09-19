package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"bluecode/tests/harness"
)

// TestPrograms builds every program of the suite for both CPUs and loads the pod on this one:
// a program that fails to compile, that needs a relocation, or that the runtime refuses, fails
// here. The programs are the specification of the language, so all of them are built.
func TestPrograms(t *testing.T) {
	programs, err := filepath.Glob("programs/*.bc")
	if err != nil {
		t.Fatal(err)
	}

	if len(programs) == 0 {
		t.Fatal("no program in programs/")
	}

	for _, program := range programs {
		t.Run(strings.TrimSuffix(filepath.Base(program), ".bc"), func(t *testing.T) {
			harness.Build(t, program)
		})
	}
}
