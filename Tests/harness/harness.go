package harness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// cpus names the CPU a Go program runs on the way the compiler and the pod files name it.
var cpus = map[string]string{"arm64": "aarch64", "amd64": "x86_64"}

// architectures are the CPUs every program is built for: a pod has to compile for both, and
// only then is a language change done.
var architectures = []string{"aarch64", "x86_64"}

// testsRoot locates the Tests module once; every program path is relative to it.
var testsRoot = sync.OnceValues(findRoot)

// findRoot walks up from the working directory to the directory holding go.mod, so the harness
// works whatever directory a test is run from.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("harness: working directory:\n%w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("harness: no go.mod above the working directory")
		}

		dir = parent
	}
}

// publishDir is where the compiler lays its single-file binary and the pod recipe beside it.
func publishDir(root string) string {
	return filepath.Join(root, "..", "Compiler", "bin", "Release", "net10.0", "osx-arm64", "publish")
}

// compilerPath returns the published compiler, or an error saying how to produce it.
func compilerPath(root string) (string, error) {
	path := filepath.Join(publishDir(root), "Compiler")

	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("harness: no compiler at %s: run `just publish`", path)
	}

	return path, nil
}

// podJustfile returns the recipe that turns a source into a pod. It is the single source of
// truth for the clang flags, so the harness never calls clang itself.
func podJustfile(root string) string {
	return filepath.Join(publishDir(root), "justfile")
}
