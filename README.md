<p align="center">
  <img src="docs/sledge-logo.png" alt="sledge" width="560">
</p>

# sledge

`sledge` load-tests MCP servers over Streamable HTTP. Custom headers and OAuth are first-class. It is a Go CLI, not a k6 wrapper.

Closed-model VUs. One session per VU by default. Per-VU HTTP clients by default. Shared OAuth token by default. Initialize is implicit.

First run is 1 VU. Keep real URLs and keys out of git. Longer notes: [docs/getting-started.md](docs/getting-started.md).

## Install

Go 1.22+.

```bash
go install github.com/pmdroid/sledge/cmd/sledge@main
```

From a clone:

```bash
git clone https://github.com/pmdroid/sledge.git
cd sledge
make build          # writes bin/sledge
```

`sledge version` prints `0.0.0-dev`.

## Commands

```
sledge <run|validate|version> [scenario]
```

| Command | What it does |
|---|---|
| `version` | Print the version string. Exit 0. |
| `validate <file>` | Load and check a scenario YAML. Exit 0 or 2. |
| `run <file>` | Run the scenario. Text report on stdout. |
| `auth <file>` | MCP OAuth authorization-code login. Stores a refresh token. |

Unknown command, missing path, or bad flags: exit 2.

### `validate`

```
sledge validate scenario.yaml
sledge validate --vus 1 --duration 10s scenario.yaml
```

`--vus` and `--duration` override file values the same way `run` does, then validate the result.

### `auth`

```
sledge auth scenario.yaml
```

For `grant: authorization_code` (or `mcp`). Discovers the authorization server from the MCP URL, registers a public client if `client_id` is empty, and waits on a localhost callback. The refresh token is written under your user state dir as mode `0600`. `sledge run` reads it back. Not per VU.

### `run`

```
sledge run scenario.yaml
sledge run --vus 1 --duration 10s scenario.yaml
sledge run --http-shared-pool --out report.json scenario.yaml
```

| Flag | Effect |
|---|---|
| `--vus N` | Override `workload.vus`. `0` leaves the file value. |
| `--duration D` | Override `workload.duration`. Go duration (`10s`, `2m`). Empty leaves the file value. |
| `--http-shared-pool` | One shared `http.Client` for every VU. Same as `http.pool: shared`. |
| `--out PATH` / `--out-file PATH` | Write the JSON report to `PATH` mode `0600`. Aliases. |
| `--progress` | Live status on stderr once per second (iters, ops, errs, rps, p95). |
| `--insecure-log-secrets` | See [Redaction](#redaction). |

## First run

Copy [examples/first-run.yaml](examples/first-run.yaml). The host and secrets stay in the environment.

```yaml
version: 1
target:
  url: ${env:MCP_URL}
  transport: streamable-http
  headers:
    Authorization: Bearer ${secret:API_KEY}
    Arcade-User-ID: ${env:ARCADE_USER_ID}
workload:
  model: closed
  vus: 1
  duration: 30s
  iterations: 1
  session:
    mode: per_vu
steps:
  - tools/list: {}
  - tools/call:
      name: ${env:MCP_TOOL}
      arguments: {}
    expect:
      ok: true
      max_duration: 30s
```

```bash
export MCP_URL='https://example.com/mcp'
export API_KEY='your-token'
export ARCADE_USER_ID='you@example.com'
export MCP_TOOL='Arcade_ListApps'

sledge validate examples/first-run.yaml
sledge run --vus 1 --duration 30s examples/first-run.yaml
```

`iterations: 1` is initialize, one pass over the steps, then close. Set `MCP_TOOL` to a name from `tools/list`. Drop `Bearer ` if the server wants the raw key. For OAuth instead of a static header, see [docs/getting-started.md](docs/getting-started.md). Full YAML: [docs/scenario.md](docs/scenario.md).

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

Supported grants: `client_credentials`, pre-seeded `refresh_token`, and MCP OAuth `authorization_code` (`mcp` is an alias). Browser login is `sledge auth <scenario>`, once per resource. The refresh token lives in user state (`0600`), not in git. `sledge run` reuses it.

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
| `grant` | required | `client_credentials`, `refresh_token`, `authorization_code`, or `mcp` |
| `token_url` | required except `authorization_code` / `mcp` | Discovered by `sledge auth` when omitted |
| `client_id` | required except `authorization_code` / `mcp` | Dynamic client registration when omitted |
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

stdio, legacy SSE as a transport, arrival-rate, distributed workers, JS scenarios, JSONPath, Prometheus, JUnit.

## Docs

- [Getting started](docs/getting-started.md)
- [Scenario YAML](docs/scenario.md)
- [Tests](docs/testing.md)
- [First-run example](examples/first-run.yaml)
- [OAuth first-run example](examples/oauth-first-run.yaml)

## Tests

In-process godog + httptest fakes. No public internet. See [docs/testing.md](docs/testing.md).

```bash
go test ./...
```
