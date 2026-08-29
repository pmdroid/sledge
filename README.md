# mcp-loadtester

CLI: `mcpload`. Load-tests MCP servers over Streamable HTTP. Custom headers and OAuth are first-class.

v1: [tracking](https://github.com/pmdroid/mcp-loadtester/issues/9). Closed-model VUs, one session per VU, per-VU HTTP clients. Shared OAuth token by default. No k6 wrapper.

## Not v1

stdio, legacy SSE, arrival-rate, distributed workers, JS scenarios, JSONPath on results, Prometheus, JUnit, interactive authorization-code.
