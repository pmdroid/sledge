# Tests

All tests stay on loopback. `httptest` fakes stand in for MCP and the token endpoint. There is no public internet in `go test`.

## Run

```bash
go test ./...
make test          # same: go test ./...
```

Package tests live next to the code under `internal/` and `cmd/sledge`. Feature tests live in `features/`.

`make build` writes `bin/sledge`. Feature acceptance compiles its own binary into a temp dir.

## Fakes

| Package | Role |
|---|---|
| `internal/fakes/mcphttp` | Streamable HTTP MCP. JSON or SSE bodies, session ids, optional delay, 401, required headers, mid-stream disconnect. URL is `http://127.0.0.1:<port>/mcp`. |
| `internal/fakes/token` | OAuth token endpoint. `client_credentials` and `refresh_token`, optional refresh rotation and short `expires_in`. URL is `http://127.0.0.1:<port>/token`. |

Unit tests in `internal/session`, `internal/oauth`, `internal/runner`, and `cmd/sledge` start these servers and point scenarios at `srv.URL()`.

## Godog

`features/harness_test.go` runs one `godog.TestSuite` named `harness` with `Strict: true` and `Paths: []string{"."}`. `TestFeatures` compiles `./cmd/sledge` once, then walks every `*.feature` in `features/`.

| Feature | What it covers |
|---|---|
| `harness.feature` | Fake MCP JSON/SSE, session header, refresh rotation. Direct HTTP, not the CLI. |
| `session.feature` | `session.Client` Initialize / Call / Close, SSE, transport tag on disconnect. |
| `oauth.feature` | Shared token singleflight, refresh-before-expiry, static bearer, `--insecure-log-secrets` warning. |
| `validate.feature` | Frozen YAML passes. Unknown transport, secret in steps, missing fields, arrival-rate. |
| `runner.feature` | Closed-model VUs, `per_iteration` sessions, shared HTTP pool. |
| `metrics.feature` | Threshold exit 1, auth vs protocol tags, JSON redaction, intended + actual latency. |
| `acceptance.feature` | Real `sledge` binary against both fakes. Headers + OAuth, 401 → exit 1. |

Step definitions are `*_steps_test.go` in the same package `features_test`. Scenario `Before` resets fakes so runs do not share servers.

## CLI tests

`cmd/sledge/main_test.go` drives `run([]string{…})` with swapped stdout/stderr. It checks exit codes for `version`, missing paths, unknown commands, `validate` of the frozen file, `--vus`/`--duration`, `--http-shared-pool`, `--out` / `--out-file`, threshold exit 1, and the insecure-secrets warning.

## What is not tested here

No live MCP gateways. No real IdP. No stdio or arrival-rate (rejected at validate). If a test needs a token or header, it uses the fake's `ClientSecret()` / a string literal local to that test.
