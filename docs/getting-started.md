# Getting started

`mcpload` is a Go CLI. It opens MCP Streamable HTTP sessions and loops the steps you list. First run is 1 VU. Do not turn the VUs up until that run is clean.

Do not put real gateway URLs, API keys, or tokens in the repo. Keep them in the environment or a local `.env` you never commit.

## Build

Go 1.22 or newer.

```bash
git clone https://github.com/pmdroid/mcp-loadtester.git
cd mcp-loadtester
make build
./bin/mcpload version
```

`go install github.com/pmdroid/mcp-loadtester/cmd/mcpload@main` also works if you want it on `PATH`.

## A 1 VU scenario

Copy [examples/first-run.yaml](../examples/first-run.yaml). The URL and secrets come from the environment.

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
```

`iterations: 1` means initialize, run the steps once, then close. `duration` is only a cap.

```bash
export MCP_URL='https://example.com/mcp'
export API_KEY='your-token'
export ARCADE_USER_ID='you@example.com'

./bin/mcpload validate examples/first-run.yaml
./bin/mcpload run --vus 1 --duration 30s examples/first-run.yaml
```

`--vus` and `--duration` override the file. Use them. Leave the committed YAML at 1 VU.

If the server wants a static header and no `Bearer ` prefix, drop the word `Bearer` and keep `${secret:API_KEY}`. If it wants OAuth instead of a static key, omit `Authorization` and add an `auth.oauth` block. See [scenario.md](scenario.md).

Some fronts reject Go's default User-Agent (Cloudflare 1010). Set `User-Agent` in `target.headers` if initialize comes back as HTTP 403 with a browser-signature error.

## What a good run looks like

Exit 0. `iterations` is 1. `errors` is 0. `unique_sessions` is 1. `setup` is the implicit initialize.

```
./bin/mcpload run --vus 1 --duration 30s --out /tmp/mcpload-report.json examples/first-run.yaml
```

`--out` writes JSON mode `0600`. The file should not contain the raw token. `${secret:…}` values print as `[redacted]`.

Exit codes: 0 pass, 1 threshold fail, 2 config or auth setup fail, 3 internal.

## Auth that actually works

**Static header.** `auth` omitted. Put `Authorization` (and any extra headers the server documents) under `target.headers`. Arcade headers mode is `Authorization: Bearer ${secret:API_KEY}` plus `Arcade-User-ID`.

**OAuth.** `client_credentials` or a pre-seeded `refresh_token`. Shared token is the default. There is no browser login. If the server only speaks authorization-code MCP OAuth, `mcpload` cannot open a session until you have a refresh token or the server is switched to headers.

A 401 with `Invalid OAuth token` and a `WWW-Authenticate` resource-metadata URL is that second case. The project API key is not an access token.

## Protocol versions

Initialize offers `2026-07-28` first (`_meta`, `Mcp-Method`, `Mcp-Name` on named calls). If that handshake is a protocol error, it retries `2025-11-25`. Later requests use whichever version the server accepted.

Servers that only speak `2025-06-18` or `2025-03-26` will fail initialize. That is intentional.

## After 1 VU works

Raise `--vus` and `--duration` on the command line. Keep `session.mode: per_vu` unless you want a new session every iteration. Add `tools/call` steps with real tool names from `tools/list`. Put thresholds in the file when you want CI to fail a regression.

```yaml
thresholds:
  error_rate: "< 0.01"
  p95_latency: "< 800ms"
  auth_errors: "== 0"
```

Full field list: [scenario.md](scenario.md). How tests work: [testing.md](testing.md).
