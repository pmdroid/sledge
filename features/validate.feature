Feature: scenario YAML validate

  Scenario: valid file passes
    Given env CLIENT_ID is "client"
    And a scenario file:
      """
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
      """
    When I validate the scenario
    Then validation succeeds

  Scenario: unknown transport fails
    Given env CLIENT_ID is "client"
    And a scenario file:
      """
      version: 1
      target:
        url: https://example.com/mcp
        transport: sse
      workload:
        model: closed
        vus: 1
        duration: 1s
      steps:
        - tools/list: {}
      """
    When I validate the scenario
    Then validation fails with "unknown transport"

  Scenario: secret in steps fails
    Given env CLIENT_ID is "client"
    And a scenario file:
      """
      version: 1
      target:
        url: https://example.com/mcp
        transport: streamable-http
      workload:
        model: closed
        vus: 1
        duration: 1s
      steps:
        - tools/call:
            name: search
            arguments: { query: "${secret:Q}" }
      """
    When I validate the scenario
    Then validation fails with "secret interpolation is not allowed"

  Scenario: missing required field fails
    Given a scenario file:
      """
      version: 1
      target:
        transport: streamable-http
      workload:
        model: closed
        vus: 1
        duration: 1s
      steps:
        - tools/list: {}
      """
    When I validate the scenario
    Then validation fails with "missing required field"

  Scenario: authorization_code grant needs no token_url
    Given a scenario file:
      """
      version: 1
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
      steps:
        - tools/list: {}
      """
    When I validate the scenario
    Then validation succeeds

  Scenario: arrival-rate is rejected
    Given a scenario file:
      """
      version: 1
      target:
        url: https://example.com/mcp
        transport: streamable-http
      workload:
        model: arrival-rate
        vus: 1
        duration: 1s
      steps:
        - tools/list: {}
      """
    When I validate the scenario
    Then validation fails with "arrival-rate is reserved, not implemented"
