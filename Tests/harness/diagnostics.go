package harness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// marker is one thing the compiler must say about one line, whether the source asks for it or
// the compiler reports it. The column is left out: it moves with the wording of the code and
// says nothing about the rule that was broken.
type marker struct {
	line    int    // 1-based line of the source
	message string // the diagnostic, without its position
}

// reported matches one line of the compiler's output: <path>:<line>:<column>: error: <message>.
var reported = regexp.MustCompile(`^(.*):(\d+):(\d+): error: (.*)$`)

// markerPrefix opens a marker in a source; its message runs to the next marker or to the end of
// the line, which is how one line can demand two diagnostics.
const markerPrefix = "// error: "

// Diagnostics checks a program that must not compile: the compiler has to reject it, emit no
// code, and report exactly the diagnostics the source marks with "// error: ".
func Diagnostics(t testing.TB, program string) {
	t.Helper()

	root, err := testsRoot()
	if err != nil {
		t.Fatal(err)
	}

	got, err := compileForDiagnostics(root, program)
	if err != nil {
		t.Fatal(err)
	}

	want, err := expectedMarkers(filepath.Join(root, program))
	if err != nil {
		t.Fatal(err)
	}

	compare(t, program, got, want)
}

// compileForDiagnostics runs the compiler alone on a source and reads back what it reported. A
// program under errors/ that compiles, or that prints anything, is a hole in the language.
func compileForDiagnostics(root, program string) ([]marker, error) {
	compiler, err := compilerPath(root)
	if err != nil {
		return nil, err
	}

	command := exec.Command(compiler, program)
	command.Dir = root

	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr

	if err := command.Run(); err != nil && command.ProcessState == nil {
		return nil, fmt.Errorf("harness: %s:\n%w", program, err)
	}

	if err := rejected(program, command, &stdout, &stderr); err != nil {
		return nil, err
	}

	return parseDiagnostics(program, stderr.String())
}

// rejected checks that the compiler refused the program instead of compiling it.
func rejected(program string, command *exec.Cmd, stdout, stderr *bytes.Buffer) error {
	if code := command.ProcessState.ExitCode(); code != 1 {
		return fmt.Errorf("harness: %s: the compiler exited with %d, expected 1:\n%s", program, code, stderr)
	}

	if stdout.Len() > 0 {
		return fmt.Errorf("harness: %s: the compiler emitted output for a program that must not compile:\n%s", program, stdout)
	}

	return nil
}

// parseDiagnostics reads the compiler's standard error, one diagnostic per line.
func parseDiagnostics(program, output string) ([]marker, error) {
	var diagnostics []marker

	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == "" {
			continue
		}

		fields := reported.FindStringSubmatch(line)
		if fields == nil {
			return nil, fmt.Errorf("harness: %s: unreadable diagnostic %q", program, line)
		}

		number, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("harness: %s: unreadable line number in %q", program, line)
		}

		diagnostics = append(diagnostics, marker{number, fields[4]})
	}

	return diagnostics, nil
}

// expectedMarkers reads the markers of a source: one per diagnostic the compiler must report on
// that line, several when one line breaks several rules.
func expectedMarkers(path string) ([]marker, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("harness: %w", err)
	}

	var markers []marker

	for number, line := range strings.Split(string(content), "\n") {
		messages := strings.Split(line, markerPrefix)

		for _, message := range messages[1:] {
			markers = append(markers, marker{number + 1, strings.TrimSpace(message)})
		}
	}

	return markers, nil
}

// compare reports every diagnostic the source does not ask for and every marker the compiler
// does not honour, so that a change in the rules names what moved and where.
func compare(t testing.TB, program string, got, want []marker) {
	t.Helper()

	if len(want) == 0 {
		t.Errorf("%s: no %q marker in the file", program, strings.TrimSpace(markerPrefix))
	}

	honoured := make([]bool, len(want))

	for _, diagnostic := range got {
		index := match(want, honoured, diagnostic)
		if index < 0 {
			t.Errorf("%s:%d: unexpected diagnostic: %s", program, diagnostic.line, diagnostic.message)
			continue
		}

		honoured[index] = true
	}

	for index, ok := range honoured {
		if !ok {
			t.Errorf("%s:%d: marker without a diagnostic: %s", program, want[index].line, want[index].message)
		}
	}
}

// match returns the first marker of want that is still free and says the same thing.
func match(want []marker, honoured []bool, diagnostic marker) int {
	for index, candidate := range want {
		if !honoured[index] && candidate == diagnostic {
			return index
		}
	}

	return -1
}
