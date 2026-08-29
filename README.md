# mcp-loadtester

`mcpload` is a custom Go CLI that load-tests MCP servers over Streamable HTTP. Module `github.com/pmdroid/mcp-loadtester`. It is not a k6 wrapper.

v1 is closed-model VUs, implicit initialize, one session per VU by default, per-VU HTTP clients by default, shared OAuth token by default. Custom headers and OAuth are first-class.

The official Go MCP SDK is not on the request path. The seam is `Session`: Initialize, Call, Close.

## Install

Go 1.22+.

```bash
go install github.com/pmdroid/mcp-loadtester/cmd/mcpload@feat/v1
```

From a clone:

```bash
git clone https://github.com/pmdroid/mcp-loadtester.git
cd mcp-loadtester
git checkout feat/v1
make build          # writes bin/mcpload
# or: go build -o bin/mcpload ./cmd/mcpload
```

`mcpload version` prints `0.0.0-dev`.

## Commands

```
mcpload <run|validate|version> [scenario]
```

| Command | What it does |
|---|---|
| `version` | Print the version string. Exit 0. |
| `validate <file>` | Load and check a scenario YAML. Exit 0 or 2. |
| `run <file>` | Run the scenario. Text report on stdout. |

Unknown command, missing path, or bad flags: exit 2.

### `validate`

```
mcpload validate scenario.yaml
mcpload validate --vus 1 --duration 10s scenario.yaml
```

`--vus` and `--duration` override file values the same way `run` does, then validate the result.

### `run`

```
mcpload run scenario.yaml
mcpload run --vus 1 --duration 10s scenario.yaml
mcpload run --http-shared-pool --out report.json scenario.yaml
```

| Flag | Effect |
|---|---|
| `--vus N` | Override `workload.vus`. `0` leaves the file value. |
| `--duration D` | Override `workload.duration`. Go duration (`10s`, `2m`). Empty leaves the file value. |
| `--http-shared-pool` | One shared `http.Client` for every VU. Same as `http.pool: shared`. |
| `--out PATH` / `--out-file PATH` | Write the JSON report to `PATH` mode `0600`. Aliases. |
| `--insecure-log-secrets` | See [Redaction](#redaction). |

## First run

Keep the first run at 1 VU. Point `target.url` at your MCP endpoint. Do not commit real hostnames, tokens, or keys.

```yaml
version: 1
target:
  url: https://example.com/mcp
  transport: streamable-http
workload:
  model: closed
  vus: 1
  duration: 10s
  session:
    mode: per_vu
steps:
  - tools/list: {}
```

```bash
mcpload validate scenario.yaml
mcpload run --vus 1 --duration 10s scenario.yaml
```

A fuller file with OAuth, secrets, and thresholds is in [docs/scenario.md](docs/scenario.md).

## Scenario YAML

`version` must be `1`. Required: `target.url`, `target.transport`, `workload.model`, `workload.vus`, `workload.duration`, `steps`.

`target.transport` must be `streamable-http`. Anything else fails validate.

`workload.model` must be `closed`. `arrival-rate`, `open`, or a `workload.arrival_rate` / `workload.rate` field fails with `arrival-rate is reserved, not implemented`.

CLI `--vus` and `--duration` beat the file. Full field list, interpolator rules, and a complete example: [docs/scenario.md](docs/scenario.md).

## Interpolator

Only these tokens are legal. `$$` becomes a literal `$`. A bare `$` is an error.

| Token | Meaning |
|---|---|
| `${env:NAME}` | Process environment. Missing at load fails validate. |
| `${secret:NAME}` | Secret whose value is the env var `NAME`. See below. |
| `${var:NAME}` | `vars` map in the file. |
| `${vu.id}` | VU index, `0` … `vus-1`. Bound at run. |
| `${iteration}` | Iteration number starting at `1`. Bound at run. |

Names after `env:`, `secret:`, and `var:` must be identifiers: letter or `_`, then letters, digits, `_`.

`${secret:...}` is legal only in header values and OAuth fields (`target.headers.*`, `auth.oauth.token_url`, `client_id`, `client_secret`, `refresh_token`). Validate rejects it anywhere else with `secret interpolation is not allowed in <path>`.

## Secrets

`${secret:FOO}` reads env `FOO`. The `secret.Secret` type prints `[redacted]` from `String`, JSON, and YAML. `Reveal()` is used only when injecting headers and encoding the token form.

Validate does not require the env var to be set. `run` looks it up again when it builds the OAuth manager and when it sets headers.

## OAuth

Optional. Static `Authorization` headers work without an `auth` block.

Supported grants: `client_credentials` and pre-seeded `refresh_token`. There is no browser authorization-code flow.

```yaml
auth:
  oauth:
    grant: client_credentials
    token_url: https://idp.example/token
    client_id: ${env:CLIENT_ID}
    client_secret: ${secret:CLIENT_SECRET}
    scopes: [mcp.read]
    token_scope: shared
    refresh_skew: 30s
```

| Field | Default | Notes |
|---|---|---|
| `grant` | required | `client_credentials` or `refresh_token` |
| `token_url` | required | |
| `client_id` | required | |
| `client_secret` | required for `client_credentials` | |
| `refresh_token` | required for `refresh_token` | Seeded value. Rotated refresh tokens from the IdP replace it in memory. |
| `scopes` | empty | Sent as `scope` on client_credentials. Space-joined. |
| `token_scope` | `shared` | `shared` or `per_vu`. `per-vu` is accepted and stored as `per_vu`. |
| `refresh_skew` | `30s` | Refresh when `now >= expiry - skew`. |

`token_scope: shared` fetches one token for the run. Concurrent VUs wait on a singleflight. `per_vu` fetches one token per VU and prints `WARNING: token_scope per_vu fetches one token per VU and may overload the identity provider` on stderr.

Successful fetches set `Authorization: Bearer <access_token>` on MCP requests.

## Workload

Closed model only. Each VU loops steps until `duration` elapses, or until `iterations` if that is `> 0`.

| Field | Default | Notes |
|---|---|---|
| `model` | required | `closed` |
| `vus` | required | Must be `> 0` |
| `duration` | required | Go duration |
| `ramp_up` | `0` | VU `i` waits `i * ramp_up / vus`. VU `0` starts immediately. |
| `think_time` | `0` | Sleep after each iteration |
| `iterations` | `0` | `0` means duration-only. `>= 0`. |
| `session.mode` | `per_vu` | `per_vu` or `per_iteration` |

`per_vu`: initialize once, reuse the session. `per_iteration`: close and initialize every loop.

Initialize is implicit. Setup latency is a `setup` metric. It is not in `p95_latency`.

HTTP pool: `http.pool: vu` (default, one client per VU) or `http.pool: shared`. `--http-shared-pool` forces shared.

## Thresholds

Only these names. Unknown names fail the run as a config error (exit 2).

| Name | Compared against |
|---|---|
| `error_rate` | `(transport + auth + protocol + assertion) / (ops + setups)` |
| `p95_latency` | HDR p95 of intended-send latency for step operations, not setup |
| `auth_errors` | Count of failures tagged `auth` |

Expr: `<=`, `>=`, `==`, `!=`, `<`, `>` plus a number or a Go duration (`< 800ms`).

```yaml
thresholds:
  error_rate: "< 0.01"
  p95_latency: "< 800ms"
  auth_errors: "== 0"
```

Step `expect.ok: true` fails the step as `assertion` if the tool result has `isError: true`. `expect.max_duration` is an `assertion` if actual-send latency is above the cap.

## Failures

Every failed request is tagged one of `transport`, `auth`, `protocol`, `assertion`. HTTP 401/403 is `auth`. Other 4xx/5xx and JSON-RPC errors are `protocol`. Incomplete SSE is `transport`.

## Reports

Text always goes to stdout. JSON only if `--out` / `--out-file` is set.

Each latency group reports HDR p50/p90/p95/p99 twice: intended-send (from the planned send time, including wait behind a stall) and uncorrected actual-send (from the moment the HTTP request is issued).

Text includes a note that closed-model intended-send latency understates tails when the server stalls.

JSON object fields: `vus`, `duration`, `iterations`, `throughput_rps`, `error_rate`, `peak_sessions`, `saturated_vus`, `unique_sessions`, `http_pool`, `http_clients`, `setup_count`, `errors`, `failures` (`transport`/`auth`/`protocol`/`assertion`), `p95_latency`, `setup`, `operations`, `tools`, `thresholds`, `failed`, `note`. Each of `setup` / `operations` / `tools` is `{ intended, actual }` with `count` and `p50`/`p90`/`p95`/`p99` duration strings.

## Exit codes

| Code | When |
|---|---|
| 0 | Run finished, thresholds passed or none set |
| 1 | Run finished, a threshold failed |
| 2 | Usage, missing file, validate/load error, bad threshold expression, or a returned auth error |
| 3 | Other internal error from the run |

Per-VU 401s during the loop are counted as `auth` failures. They exit 1 if `auth_errors` or `error_rate` fails, not 2.

## Redaction

Secrets and access tokens are watched and replaced with `[redacted]` in the JSON report.

`--insecure-log-secrets` prints this to stderr before the run, then leaves secret bytes in logs and JSON:

```
WARNING: --insecure-log-secrets is enabled; secrets and access tokens will be written to logs and JSON reports
```

Do not turn it on against a real IdP unless you mean to leak tokens.

## Not v1

stdio, legacy SSE as a transport, arrival-rate, distributed workers, JS scenarios, JSONPath, Prometheus, JUnit, interactive authorization-code.

## Tests

In-process godog + httptest fakes. No public internet. See [docs/testing.md](docs/testing.md).

```bash
go test ./...
```
