package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"bluecode/tests/harness"
)

// TestErrors checks the programs that must not compile: each one has to be rejected with
// exactly the diagnostics its "// error: " markers describe, no more and no fewer.
func TestErrors(t *testing.T) {
	programs, err := filepath.Glob("programs/errors/*.bc")
	if err != nil {
		t.Fatal(err)
	}

	if len(programs) == 0 {
		t.Fatal("no program in programs/errors/")
	}

	for _, program := range programs {
		t.Run(strings.TrimSuffix(filepath.Base(program), ".bc"), func(t *testing.T) {
			harness.Diagnostics(t, program)
		})
	}
}
