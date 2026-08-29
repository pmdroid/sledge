Feature: OAuth client-credentials and refresh-token

  Scenario: 20 VUs share one token
    Given a fake token endpoint
    And a fake MCP server using JSON responses requiring bearer
    And an oauth manager with grant client_credentials and token_scope shared
    When 20 VUs fetch a token and initialize MCP
    Then the token endpoint recorded 1 request
    And no VU saw an auth error
    And the known token string is absent from debug logs and the JSON report

  Scenario: expiring token refreshes without an auth error
    Given a fake token endpoint with 1s expiry
    And a fake MCP server using JSON responses requiring bearer
    And an oauth manager with grant client_credentials and token_scope shared
    When a VU initializes MCP twice
    Then the token endpoint recorded 2 requests
    And no VU saw an auth error

  Scenario: rotating refresh token does not 401 the MCP
    Given a fake token endpoint with refresh rotation and 1s expiry
    And a seeded refresh token
    And a fake MCP server using JSON responses requiring bearer
    And an oauth manager with grant refresh_token and token_scope shared
    When a VU initializes MCP then lists tools
    Then the token endpoint recorded 2 requests
    And no VU saw an auth error
    And the last MCP response is application/json

  Scenario: static bearer secret header without oauth
    Given a fake MCP server using JSON responses requiring bearer token "static-token"
    And the session client headers:
      | Authorization | Bearer static-token |
    When I open a streamable session
    And I initialize through the client
    Then the last MCP response is application/json
    And the last client error is tagged ""

  Scenario: insecure-log-secrets warns and may include the token
    Given a fake token endpoint
    And insecure log secrets is enabled
    And an oauth manager with grant client_credentials and token_scope shared
    When a VU fetches a token
    Then a loud insecure-log-secrets warning was printed
    And the known token string is present in debug logs
