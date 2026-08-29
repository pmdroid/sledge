package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmdroid/sledge/internal/fakes/mcphttp"
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

func TestInsecureLogSecretsWarns(t *testing.T) {
	var errBuf bytes.Buffer
	stdout = ioDiscard()
	stderr = &errBuf
	t.Cleanup(restoreIO)
	code := run([]string{"run", "--insecure-log-secrets", "scenario.yaml"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "WARNING") || !strings.Contains(errBuf.String(), "insecure-log-secrets") {
		t.Fatalf("stderr %q", errBuf.String())
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

func TestRunMissingFile(t *testing.T) {
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"run", "scenario.yaml"})
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
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

func TestRunAgainstFake(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	body := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
workload:
  model: closed
  vus: 2
  duration: 300ms
  session:
    mode: per_vu
steps:
  - tools/list: {}
`, srv.URL())
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	stdout = &out
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"run", "--vus", "2", "--duration", "300ms", "--http-shared-pool", path})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "saturated_vus:") {
		t.Fatalf("summary %q", out.String())
	}
	if !strings.Contains(out.String(), "http_pool: shared") {
		t.Fatalf("summary %q", out.String())
	}
}

func TestRunP95ThresholdExit1(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON, Delay: 40 * time.Millisecond})
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	body := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
workload:
  model: closed
  vus: 1
  duration: 250ms
  session:
    mode: per_vu
steps:
  - tools/list: {}
thresholds:
  p95_latency: "< 1ms"
`, srv.URL())
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "out.json")
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"run", "--out", outFile, path})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"intended"`) || !strings.Contains(string(raw), `"actual"`) {
		t.Fatalf("json %s", raw)
	}
}

func TestRunOutFileAlias(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	body := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
workload:
  model: closed
  vus: 1
  duration: 150ms
  iterations: 1
  session:
    mode: per_vu
steps:
  - tools/list: {}
`, srv.URL())
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "report.json")
	stdout = ioDiscard()
	stderr = ioDiscard()
	t.Cleanup(restoreIO)
	code := run([]string{"run", "--out-file", outFile, path})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatal(err)
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
