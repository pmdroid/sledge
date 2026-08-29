package main

import (
	"bytes"
	"os"
	"path/filepath"
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
	t.Setenv("CLIENT_ID", "client")
	path := writeValidScenario(t)
	var errBuf bytes.Buffer
	stdout = ioDiscard()
	stderr = &errBuf
	t.Cleanup(restoreIO)
	code := run([]string{"validate", path})
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, errBuf.String())
	}
}

func TestValidateOverrides(t *testing.T) {
	t.Setenv("CLIENT_ID", "client")
	path := writeValidScenario(t)
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"validate", "--vus", "5", "--duration", "10s", path})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
}

func TestValidateMissingFile(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"validate", "scenario.yaml"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func writeValidScenario(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	body := []byte(`version: 1
target:
  url: https://example.com/mcp
  transport: streamable-http
  headers:
    Authorization: ${secret:STATIC_TOKEN}
auth:
  oauth:
    grant: client_credentials
    token_url: https://idp/token
    client_id: ${env:CLIENT_ID}
    client_secret: ${secret:CLIENT_SECRET}
    scopes: [mcp.read]
    token_scope: shared
    refresh_skew: 30s
workload:
  model: closed
  vus: 20
  duration: 2m
  ramp_up: 10s
  think_time: 500ms
  session:
    mode: per_vu
steps:
  - tools/list: {}
  - tools/call:
      name: search
      arguments: { query: "${var:query}" }
    expect:
      ok: true
      max_duration: 5s
vars:
  query: hello
thresholds:
  error_rate: "< 0.01"
  p95_latency: "< 800ms"
  auth_errors: "== 0"
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
