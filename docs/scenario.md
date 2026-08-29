# Scenario YAML

v1 files are `version: 1`. `mcpload validate` must accept the frozen shape below. CLI `--vus` and `--duration` override `workload.vus` and `workload.duration` after parse.

## Frozen shape

```yaml
version: 1
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
```

`auth`, `headers`, `vars`, `thresholds`, `ramp_up`, `think_time`, `session`, and `expect` are optional. A first run can be 1 VU, no auth, one step.

## Required

| Field | Rule |
|---|---|
| `version` | `1` |
| `target.url` | Non-empty. Interpolated. Secrets not allowed. |
| `target.transport` | `streamable-http` only |
| `workload.model` | `closed` |
| `workload.vus` | Integer `> 0` |
| `workload.duration` | Go duration `> 0` |
| `steps` | At least one mapping whose key is the JSON-RPC method |

Missing any of those: `missing required field: …`.

Unsupported `version`: `unsupported version N`.

## Target

`transport: streamable-http` is the only legal value. `sse`, `stdio`, and anything else: `unknown transport "…"; only streamable-http is legal`.

`headers` is a string map. Values go through the interpolator. Secrets are allowed here.

The client also sets `Accept: application/json, text/event-stream`, `Content-Type: application/json` on POST, `MCP-Protocol-Version: 2025-03-26`, and `Mcp-Session-Id` after initialize. OAuth, if configured, sets `Authorization: Bearer …` and will overwrite a header of the same name.

## HTTP pool

```yaml
http:
  pool: vu
```

| Value | Meaning |
|---|---|
| `vu` | Default. One `http.Client` per VU. |
| `shared` | One client for the run. |

`--http-shared-pool` sets `shared`. Unknown values fail validate.

## Auth

Omit `auth` for static headers only.

`auth.oauth.grant`: `client_credentials` or `refresh_token`. Anything else, including authorization-code: `unknown oauth grant`.

`token_scope`: `shared` (default) or `per_vu`. `per-vu` is stored as `per_vu`.

`refresh_skew` parses as a Go duration. Empty or zero becomes `30s`.

`client_credentials` requires `client_secret` as text or `${secret:…}`. `refresh_token` requires `refresh_token` the same way.

## Workload

`model: closed` only.

These all fail with `arrival-rate is reserved, not implemented`:

- `model: arrival-rate`
- `model: open`
- any other non-`closed` model
- a present `workload.arrival_rate` key
- a present `workload.rate` key

`session.mode`: `per_vu` (default) or `per_iteration`. Unknown: `unknown session.mode`.

`iterations`, if set, must be `>= 0`. `0` means run until `duration`.

Durations use Go parse: `500ms`, `10s`, `2m`.

## Steps

Each step is a one-key mapping. The key is the method. The value is the JSON-RPC `params` object.

```yaml
steps:
  - tools/list: {}
  - tools/call:
      name: search
      arguments:
        query: "${var:query}"
    expect:
      ok: true
      max_duration: 5s
```

A second method key on the same step: `step has multiple methods`.

`expect.ok: true` tags `assertion` if the result JSON has `isError: true`. `expect.max_duration` tags `assertion` if actual-send latency exceeds it.

Initialize is not a step. The runner calls it on session open.

`${vu.id}` and `${iteration}` in step strings are substituted per VU per loop. `${var:…}` and `${env:…}` are bound when the file loads.

Secrets are not allowed in step bodies.

## Vars and thresholds

`vars` values may use `${env:…}`. Secrets are not allowed. After load, `${var:name}` expands to that string.

Threshold names: `error_rate`, `p95_latency`, `auth_errors`. Operators: `<`, `<=`, `>`, `>=`, `==`, `!=`. `p95_latency` takes a duration. The others take a number.

Unknown threshold name is a config error at run (exit 2), not a validate-time check unless interpolator rules fail.

## Interpolation

Only:

```
${env:X}  ${secret:X}  ${var:X}  ${vu.id}  ${iteration}
```

`$$` is a literal `$`. Unescaped `$`, unclosed `${…}`, empty names, and unknown keys (`${foo:bar}`) fail parse.

Secret tokens are allowed only in:

- `target.headers.*`
- `auth.oauth.token_url`
- `auth.oauth.client_id`
- `auth.oauth.client_secret`
- `auth.oauth.refresh_token`

Everywhere else, including `target.url`, `vars`, `steps`, and `thresholds`: `secret interpolation is not allowed in <path>`.

`${secret:NAME}` is filled from environment variable `NAME`. Print, JSON, and YAML show `[redacted]`. The raw bytes are used only for header inject and token-form encode.

`${env:NAME}` missing at load: `environment variable NAME is not set`.

`${vu.id}` is `0` … `vus-1`. `${iteration}` starts at `1`. Unbound placeholders stringify as themselves until run.

## Complete example

Placeholder URLs and env/secret names only. First-run knobs are 1 VU.

```yaml
version: 1
target:
  url: https://example.com/mcp
  transport: streamable-http
  headers:
    X-Team: platform
    Authorization: ${secret:STATIC_TOKEN}
auth:
  oauth:
    grant: client_credentials
    token_url: https://idp.example/token
    client_id: ${env:CLIENT_ID}
    client_secret: ${secret:CLIENT_SECRET}
    scopes: [mcp.read]
    token_scope: shared
    refresh_skew: 30s
http:
  pool: vu
workload:
  model: closed
  vus: 1
  duration: 10s
  ramp_up: 0s
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
```

```bash
export CLIENT_ID=example-client
export CLIENT_SECRET=   # set locally, never commit
export STATIC_TOKEN=    # set locally, never commit
mcpload validate --vus 1 --duration 10s scenario.yaml
mcpload run --vus 1 --duration 10s scenario.yaml
```

Refresh-token grant, same placeholder hosts:

```yaml
auth:
  oauth:
    grant: refresh_token
    token_url: https://idp.example/token
    client_id: ${env:CLIENT_ID}
    client_secret: ${secret:CLIENT_SECRET}
    refresh_token: ${secret:REFRESH_TOKEN}
    token_scope: shared
    refresh_skew: 30s
```

Do not put real gateway URLs, tokens, or API keys in this file or in git.
