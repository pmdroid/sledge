package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	stdout = &out
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"version"})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := strings.TrimSpace(out.String())
	if got == "" {
		t.Fatal("empty version")
	}
}

func TestRunMissingPath(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"run"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestValidateMissingPath(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"validate"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunWithPath(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"run", "scenario.yaml"})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
}

func TestValidateWithPath(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"validate", "scenario.yaml"})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
}

func TestUnknownCommand(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"nope"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestNoArgs(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run(nil)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func restoreIO() {
	stdout = osStdout
	stderr = osStderr
}

var (
	osStdout = stdout
	osStderr = stderr
)

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}
