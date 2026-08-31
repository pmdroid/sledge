# Getting started

`sledge` is a Go CLI. It opens MCP Streamable HTTP sessions and loops the steps you list. First run is 1 VU. Do not turn the VUs up until that run is clean.

Do not put real gateway URLs, API keys, or tokens in the repo. Keep them in the environment or a local `.env` you never commit.

## Build

Go 1.22 or newer.

```bash
git clone https://github.com/pmdroid/sledge.git
cd sledge
make build
./bin/sledge version
```

`go install github.com/pmdroid/sledge/cmd/sledge@main` also works if you want it on `PATH`.

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
  - tools/call:
      name: ${env:MCP_TOOL}
      arguments: {}
    expect:
      ok: true
      max_duration: 30s
```

`iterations: 1` means initialize, run the steps once, then close. `duration` is only a cap.

```bash
export MCP_URL='https://example.com/mcp'
export API_KEY='your-token'
export ARCADE_USER_ID='you@example.com'
export MCP_TOOL='Arcade_ListApps'

./bin/sledge validate examples/first-run.yaml
./bin/sledge run --vus 1 --duration 30s examples/first-run.yaml
```

`--vus` and `--duration` override the file. Use them. Leave the committed YAML at 1 VU. Pick `MCP_TOOL` from a manual `tools/list` against your server (read-only tools are safest for smoke tests).

If the server wants a static header and no `Bearer ` prefix, drop the word `Bearer` and keep `${secret:API_KEY}`. If it wants OAuth instead of a static key, omit `Authorization` and add an `auth.oauth` block. See [scenario.md](scenario.md).

The client sends `User-Agent: sledge/0.0.0-dev` unless you set `User-Agent` in `target.headers`. That is enough to get past Cloudflare 1010 on fronts that reject Go's default. Override it if a host wants a different string.

## What a good run looks like

Exit 0. `iterations` is 1. `errors` is 0. `unique_sessions` is 1. `setup` is the implicit initialize.

```
./bin/sledge run --vus 1 --duration 30s --out /tmp/sledge-report.json examples/first-run.yaml
```

`--out` writes JSON mode `0600`. The file should not contain the raw token. `${secret:…}` values print as `[redacted]`.

Use `--progress` for a live status line on stderr (once per second: elapsed, iterations, ops, errors, rps, p95). The final text report still goes to stdout when the run finishes. With a TTY, the report uses sledge-themed colors (orange/green/cyan); pipe to a file or set `--color never` for plain text.

Exit codes: 0 pass, 1 threshold fail, 2 config or auth setup fail, 3 internal.

## Auth that actually works

**Static header.** `auth` omitted. Put `Authorization` (and any extra headers the server documents) under `target.headers`. Arcade headers mode is `Authorization: Bearer ${secret:API_KEY}` plus `Arcade-User-ID`.

**OAuth.** `client_credentials` or a pre-seeded `refresh_token` work as before. Shared token is the default.

If the server only speaks authorization-code MCP OAuth (401 plus `WWW-Authenticate` with `resource_metadata=`), the project API key is not an access token. Use:

```yaml
auth:
  oauth:
    grant: authorization_code
    token_scope: shared
```

```bash
sledge auth examples/oauth-first-run.yaml
export MCP_TOOL='Arcade_ListApps'
sledge run --vus 1 --duration 30s examples/oauth-first-run.yaml
```

`sledge auth` discovers the protected-resource and authorization-server metadata, registers a public client if `client_id` is empty, and opens a localhost callback for the authorization-code + PKCE login. The refresh token is written under your user state dir (`$SLEDGE_STATE_DIR/tokens` or `UserConfigDir/sledge/tokens`) as mode `0600`. It is not committed. `sledge run` reuses that file. Auth is once per resource, not per VU.

## Protocol versions

Initialize offers `2026-07-28` first (`_meta` on every request, `Mcp-Method`, `Mcp-Name` on named calls, no session id). If initialize fails, it tries `server/discover`, then `2025-11-25`. Classic `2025-11-25` still uses a session and `notifications/initialized`.

Servers that only speak `2025-06-18` or `2025-03-26` will fail initialize. That is intentional.

## After 1 VU works

Raise `--vus` and `--duration` on the command line. Keep `session.mode: per_vu` unless you want a new session every iteration. Every scenario needs at least one `tools/call` step (list-only YAML fails validate). Put thresholds in the file when you want CI to fail a regression.

```yaml
thresholds:
  error_rate: "< 0.01"
  p95_latency: "< 800ms"
  auth_errors: "== 0"
```

Full field list: [scenario.md](scenario.md). How tests work: [testing.md](testing.md).
