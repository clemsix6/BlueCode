package harness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Artifacts are the files a program compiles into: one object per CPU, and the manifest the
// runtime reads beside them.
type Artifacts struct {
	Native   string            // object built for the CPU the tests run on
	Foreign  string            // object built for the other CPU, compiled but never loaded
	Manifest string            // JSON manifest, the contract between compiler and runtime
	objects  map[string]string // every object, by the CPU name the compiler uses
}

// Object returns the object built for one CPU, named as the compiler names it.
func (a Artifacts) Object(arch string) string {
	return a.objects[arch]
}

// build holds the one compilation of a program, whether it succeeded or not.
type build struct {
	once      sync.Once // guards the compilation itself
	artifacts Artifacts // what it produced
	err       error     // why it produced nothing
}

// builds caches one build per program path.
var builds sync.Map

// Compile builds a program for both CPUs and verifies the two objects. A program is built once
// per process, so several tests sharing one program pay for it once and a broken program fails
// them all with the same message.
func Compile(t testing.TB, program string) Artifacts {
	t.Helper()

	entry, _ := builds.LoadOrStore(program, &build{})
	cached := entry.(*build)

	cached.once.Do(func() { cached.artifacts, cached.err = buildProgram(program) })

	if cached.err != nil {
		t.Fatal(cached.err)
	}

	return cached.artifacts
}

// buildProgram stages a program in build/, keeps its IR next to it and turns it into a pod for
// every CPU.
func buildProgram(program string) (Artifacts, error) {
	root, err := testsRoot()
	if err != nil {
		return Artifacts{}, err
	}

	source, err := stageSource(root, program)
	if err != nil {
		return Artifacts{}, err
	}

	if err := writeIr(root, source); err != nil {
		return Artifacts{}, err
	}

	return buildPods(root, source)
}

// stageSource copies a program into the flat build directory, so that everything generated from
// it lands in one place and nothing is ever written next to the sources.
func stageSource(root, program string) (string, error) {
	content, err := os.ReadFile(filepath.Join(root, program))
	if err != nil {
		return "", fmt.Errorf("harness: %w", err)
	}

	dir := filepath.Join(root, "build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("harness: %w", err)
	}

	source := filepath.Join(dir, filepath.Base(program))
	if err := os.WriteFile(source, content, 0o644); err != nil {
		return "", fmt.Errorf("harness: %w", err)
	}

	return source, nil
}

// writeIr keeps the IR of a program beside its objects: it is what a pod that misbehaves is
// read back from.
func writeIr(root, source string) error {
	compiler, err := compilerPath(root)
	if err != nil {
		return err
	}

	ir, err := run(exec.Command(compiler, source))
	if err != nil {
		return err
	}

	path := strings.TrimSuffix(source, ".bc") + ".ll"
	if err := os.WriteFile(path, ir, 0o644); err != nil {
		return fmt.Errorf("harness: %w", err)
	}

	return nil
}

// buildPods runs the published recipe for every CPU and verifies what came out: a program that
// only builds for one CPU, or that carries a relocation, is not a pod.
func buildPods(root, source string) (Artifacts, error) {
	name := strings.TrimSuffix(source, ".bc")
	objects := make(map[string]string, len(architectures))

	for _, arch := range architectures {
		if _, err := run(exec.Command("just", "-f", podJustfile(root), "pod", source, arch)); err != nil {
			return Artifacts{}, err
		}

		object := fmt.Sprintf("%s.%s.o", name, arch)
		if err := Verify(object); err != nil {
			return Artifacts{}, err
		}

		objects[arch] = object
	}

	return artifactsOf(objects, name+".json"), nil
}

// artifactsOf sorts the objects into the one this CPU can load and the one it cannot.
func artifactsOf(objects map[string]string, manifest string) Artifacts {
	native := cpus[runtime.GOARCH]
	artifacts := Artifacts{Native: objects[native], Manifest: manifest, objects: objects}

	for arch, object := range objects {
		if arch != native {
			artifacts.Foreign = object
		}
	}

	return artifacts
}

// run executes a command and returns its standard output, or an error carrying what it printed
// on standard error: a compiler diagnostic is the whole explanation of a failed build.
func run(command *exec.Cmd) ([]byte, error) {
	var stderr bytes.Buffer
	command.Stderr = &stderr

	stdout, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("harness: %s failed: %w\n%s", filepath.Base(command.Path), err, stderr.String())
	}

	return stdout, nil
}
