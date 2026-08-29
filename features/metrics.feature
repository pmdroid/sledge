Feature: Metrics, report, thresholds, redaction

  Scenario: fake p95 above threshold exits 1
    Given a fake MCP server using JSON responses delayed 40ms
    And a closed scenario with 1 VU for 300ms with p95_latency "< 1ms"
    When I run the scenario
    Then the run exits 1
    And the JSON report has intended and actual latency
    And the text report mentions closed-model understated tails

  Scenario: forced 401 increments auth not protocol
    Given a fake MCP server using JSON responses requiring bearer token "need-token"
    And a closed scenario with 1 VU for 250ms with no auth
    When I run the scenario
    Then auth failures are greater than 0
    And protocol failures are 0

  Scenario: JSON report has no secret bytes
    Given a fake MCP server using JSON responses requiring bearer token "super-secret-token-xyz"
    And a closed scenario with static bearer "super-secret-token-xyz" and 1 VU for 250ms
    When I run the scenario
    Then the JSON report does not contain "super-secret-token-xyz"
    And intended and uncorrected latency are both present
    And the text report mentions closed-model understated tails
