package scenario

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadValid(t *testing.T) {
	t.Setenv("CLIENT_ID", "client")
	sc, err := Load(strings.NewReader(validYAML), Options{LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Workload.VUs != 20 {
		t.Fatalf("vus %d", sc.Workload.VUs)
	}
	if sc.Target.Headers["Authorization"].String() != "[redacted]" {
		t.Fatalf("header %q", sc.Target.Headers["Authorization"].String())
	}
	if sc.Steps[1].Body["arguments"].(map[string]any)["query"] != "hello" {
		t.Fatalf("query %v", sc.Steps[1].Body)
	}
}

func TestTokenScopePerVU(t *testing.T) {
	t.Setenv("CLIENT_ID", "client")
	y := strings.Replace(validYAML, "token_scope: shared", "token_scope: per_vu", 1)
	sc, err := Load(strings.NewReader(y), Options{LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Auth.OAuth.TokenScope != "per_vu" {
		t.Fatalf("scope %q", sc.Auth.OAuth.TokenScope)
	}
	y = strings.Replace(validYAML, "token_scope: shared", "token_scope: per-vu", 1)
	sc, err = Load(strings.NewReader(y), Options{LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Auth.OAuth.TokenScope != "per_vu" {
		t.Fatalf("scope %q", sc.Auth.OAuth.TokenScope)
	}
}

func TestCLIOverrides(t *testing.T) {
	t.Setenv("CLIENT_ID", "client")
	sc, err := Load(strings.NewReader(validYAML), Options{VUs: 3, Duration: "15s", LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Workload.VUs != 3 {
		t.Fatalf("vus %d", sc.Workload.VUs)
	}
	if sc.Workload.Duration != 15*time.Second {
		t.Fatalf("duration %s", sc.Workload.Duration)
	}
}

const validYAML = `version: 1
target:
  url: https://example.com/mcp
  transport: streamable-http
  headers:
    X-Team: platform
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
`
