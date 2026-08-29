Feature: in-process Streamable HTTP MCP and OAuth token fakes

  Scenario: JSON MCP after client_credentials
    Given a fake token endpoint
    And a fake MCP server using JSON responses
    When I request a client_credentials token
    And I initialize an MCP session
    And I list tools
    And I call tool "search" with query "hello"
    Then the token endpoint recorded 1 request
    And the MCP server recorded 3 requests
    And subsequent MCP requests carry Mcp-Session-Id
    And the last MCP response is application/json
    And the tool call succeeded

  Scenario: SSE chunked MCP
    Given a fake token endpoint
    And a fake MCP server using SSE responses
    When I request a client_credentials token
    And I initialize an MCP session
    Then the last MCP response is text/event-stream
    And the initialize result is on the SSE stream
    And the MCP server recorded the initialize body

  Scenario: slow SSE then mid-stream disconnect
    Given a fake MCP server using slow SSE responses
    When I initialize an MCP session
    Then the last MCP response is text/event-stream
    And the initialize result is on the SSE stream
    Given a fake MCP server that disconnects mid-stream
    When I initialize an MCP session
    Then the SSE stream is incomplete

  Scenario: refresh token rotation and expiry
    Given a fake token endpoint with refresh rotation and 1s expiry
    When I request a client_credentials token
    Then the token expires in 1 second
    When I refresh the token
    Then a new refresh token is issued
    And the old refresh token is rejected
