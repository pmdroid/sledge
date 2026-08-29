Feature: v1 acceptance headers and OAuth against fakes

  Scenario: headers plus oauth pass thresholds
    Given a fake token endpoint with client_id "acc-client" and client_secret "acc-client-secret-xyz"
    And a fake MCP server requiring bearer and header "X-Team" equal to "platform"
    And a v1 scenario file with 5 VUs shared oauth and two steps
    When I run the sledge binary
    Then the run exits 0
    And the text report shows p95 and throughput
    And auth errors are 0
    And stdout and the JSON report omit the access token and client_secret
    And every recorded MCP request has header "X-Team" equal to "platform"
    And the token endpoint recorded 1 request

  Scenario: 401-ing server fails auth_errors
    Given a fake token endpoint with client_id "acc-client" and client_secret "acc-client-secret-xyz"
    And a fake MCP server that always returns 401
    And a v1 scenario file with 5 VUs shared oauth and two steps
    When I run the sledge binary
    Then the run exits 1
