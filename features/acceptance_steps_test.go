package features_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/pmdroid/mcp-loadtester/internal/fakes/mcphttp"
	"github.com/pmdroid/mcp-loadtester/internal/fakes/token"
)

var mcploadBin string

func compileMcpload(t *testing.T) (string, error) {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		root = filepath.Dir(root)
	}
	bin := filepath.Join(t.TempDir(), "mcpload")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/mcpload")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go build: %w\n%s", err, out)
	}
	return bin, nil
}

func initAcceptance(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a fake token endpoint with client_id "([^"]*)" and client_secret "([^"]*)"$`, w.startTokenCreds)
	sc.Step(`^a fake MCP server requiring bearer and header "([^"]*)" equal to "([^"]*)"$`, w.startMCPBearerHeader)
	sc.Step(`^a fake MCP server that always returns 401$`, w.startMCPAlways401)
	sc.Step(`^a v1 scenario file with 5 VUs shared oauth and two steps$`, w.writeV1Scenario)
	sc.Step(`^I run the mcpload binary$`, w.runMcploadBin)
	sc.Step(`^the text report shows p95 and throughput$`, w.textHasP95Throughput)
	sc.Step(`^auth errors are 0$`, w.authErrorsZero)
	sc.Step(`^stdout and the JSON report omit the access token and client_secret$`, w.binOmitsSecrets)
}

func (w *world) startTokenCreds(id, secret string) error {
	return w.startToken(token.Options{ClientID: id, ClientSecret: secret})
}

func (w *world) startMCPBearerHeader(key, val string) error {
	if err := w.startMCP(mcphttp.Options{Mode: mcphttp.ModeJSON}); err != nil {
		return err
	}
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	w.mcp.RequireAccess(w.tok.ValidAccess)
	w.mcp.RequireHeader(key, val)
	return nil
}

func (w *world) startMCPAlways401() error {
	if err := w.startMCP(mcphttp.Options{Mode: mcphttp.ModeJSON}); err != nil {
		return err
	}
	w.mcp.AlwaysUnauthorized()
	return nil
}

func (w *world) writeV1Scenario() error {
	if w.mcp == nil {
		return fmt.Errorf("no mcp server")
	}
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	dir, err := os.MkdirTemp("", "mcpload-acc-")
	if err != nil {
		return err
	}
	w.runDir = dir
	body := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
  headers:
    X-Team: platform
auth:
  oauth:
    grant: client_credentials
    token_url: %s
    client_id: ${env:CLIENT_ID}
    client_secret: ${secret:CLIENT_SECRET}
    scopes: [mcp.read]
    token_scope: shared
    refresh_skew: 30s
workload:
  model: closed
  vus: 5
  duration: 2m
  ramp_up: 0s
  think_time: 0s
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
`, w.mcp.URL(), w.tok.URL())
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	w.scenPath = path
	return nil
}

func (w *world) runMcploadBin() error {
	if mcploadBin == "" {
		return fmt.Errorf("mcpload binary not built")
	}
	if w.scenPath == "" {
		return fmt.Errorf("no scenario")
	}
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	outPath := filepath.Join(w.runDir, "out.json")
	cmd := exec.Command(mcploadBin, "run", "--vus", "5", "--duration", "800ms", "--out", outPath, w.scenPath)
	cmd.Env = append(os.Environ(),
		"CLIENT_ID="+w.tok.ClientID(),
		"CLIENT_SECRET="+w.tok.ClientSecret(),
	)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("mcpload timed out: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	w.runOut = stdout
	w.binErr = stderr
	if err == nil {
		w.runCode = 0
	} else if ee, ok := err.(*exec.ExitError); ok {
		w.runCode = ee.ExitCode()
	} else {
		return fmt.Errorf("mcpload: %w stderr=%s", err, stderr.String())
	}
	raw, readErr := os.ReadFile(outPath)
	if readErr == nil {
		w.binJSON = raw
	}
	return nil
}

func (w *world) textHasP95Throughput() error {
	if w.runOut == nil {
		return fmt.Errorf("no stdout")
	}
	s := w.runOut.String()
	if !strings.Contains(s, "p95_latency:") {
		return fmt.Errorf("missing p95: %q", s)
	}
	if !strings.Contains(s, "throughput_rps:") {
		return fmt.Errorf("missing throughput: %q", s)
	}
	if !strings.Contains(string(w.binJSON), `"p95"`) {
		return fmt.Errorf("json missing p95: %s", w.binJSON)
	}
	if !strings.Contains(string(w.binJSON), `"throughput_rps"`) {
		return fmt.Errorf("json missing throughput: %s", w.binJSON)
	}
	return nil
}

func (w *world) authErrorsZero() error {
	if len(w.binJSON) == 0 {
		return fmt.Errorf("no json report")
	}
	var rep struct {
		Failures struct {
			Auth int64 `json:"auth"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(w.binJSON, &rep); err != nil {
		return err
	}
	if rep.Failures.Auth != 0 {
		return fmt.Errorf("auth errors %d stdout=%s", rep.Failures.Auth, w.runOut.String())
	}
	return nil
}

func (w *world) binOmitsSecrets() error {
	blob := ""
	if w.runOut != nil {
		blob += w.runOut.String()
	}
	if w.binErr != nil {
		blob += w.binErr.String()
	}
	blob += string(w.binJSON)
	secrets := []string{w.tok.ClientSecret()}
	secrets = append(secrets, w.tok.AccessTokens()...)
	for _, s := range secrets {
		if s != "" && strings.Contains(blob, s) {
			return fmt.Errorf("leaked %q", s)
		}
	}
	if len(w.tok.AccessTokens()) == 0 {
		return fmt.Errorf("no access tokens issued")
	}
	return nil
}
