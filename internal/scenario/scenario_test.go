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

func TestHTTPPool(t *testing.T) {
	t.Setenv("CLIENT_ID", "client")
	sc, err := Load(strings.NewReader(validYAML), Options{LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.HTTP.Pool != "vu" {
		t.Fatalf("pool %q", sc.HTTP.Pool)
	}
	y := validYAML + "\nhttp:\n  pool: shared\n"
	sc, err = Load(strings.NewReader(y), Options{LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.HTTP.Pool != "shared" {
		t.Fatalf("pool %q", sc.HTTP.Pool)
	}
	sc, err = Load(strings.NewReader(validYAML), Options{SharedPool: true, LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	if sc.HTTP.Pool != "shared" {
		t.Fatalf("pool %q", sc.HTTP.Pool)
	}
}

func TestAuthCodeGrantOptionalFields(t *testing.T) {
	y := `version: 1
target:
  url: https://example.com/mcp
  transport: streamable-http
auth:
  oauth:
    grant: authorization_code
    token_scope: shared
workload:
  model: closed
  vus: 1
  duration: 1s
  session:
    mode: per_vu
steps:
  - tools/list: {}
  - tools/call:
      name: search
      arguments: { query: hello }
`
	sc, err := Load(strings.NewReader(y), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Auth.OAuth.Grant != "authorization_code" {
		t.Fatalf("grant %q", sc.Auth.OAuth.Grant)
	}
	y = strings.Replace(y, "authorization_code", "mcp", 1)
	sc, err = Load(strings.NewReader(y), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Auth.OAuth.Grant != "mcp" {
		t.Fatalf("grant %q", sc.Auth.OAuth.Grant)
	}
}

func TestRequireToolCall(t *testing.T) {
	y := `version: 1
target:
  url: https://example.com/mcp
  transport: streamable-http
workload:
  model: closed
  vus: 1
  duration: 1s
steps:
  - tools/list: {}
`
	_, err := Load(strings.NewReader(y), Options{})
	if err == nil || !strings.Contains(err.Error(), "tools/call") {
		t.Fatalf("err %v", err)
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
