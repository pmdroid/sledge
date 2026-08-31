Feature: Streamable HTTP session client

  Scenario: JSON initialize list call is stateless on 2026-07-28
    Given a fake MCP server using JSON responses
    And the session client headers:
      | X-Team | platform |
    When I open a streamable session
    And I initialize through the client
    And I list tools through the client
    And I call tool "search" through the client with query "hello"
    Then the client session id is set
    And subsequent MCP requests omit Mcp-Session-Id
    And the last MCP response is application/json
    And the tool call succeeded
    And every recorded MCP request has header "X-Team" equal to "platform"
    When I close the client session
    Then the last recorded MCP request method is "POST"
    And every recorded MCP request has header "X-Team" equal to "platform"

  Scenario: SSE-on-POST initialize and call
    Given a fake MCP server using SSE responses
    When I open a streamable session
    And I initialize through the client
    Then the last MCP response is text/event-stream
    And the initialize result is on the SSE stream
    When I list tools through the client
    Then the last MCP response is text/event-stream
    And subsequent MCP requests omit Mcp-Session-Id

  Scenario: classic 2025-11-25 uses a session id
    Given a fake MCP server using JSON responses that only supports "2025-11-25"
    When I open a streamable session
    And I initialize through the client
    And I list tools through the client
    Then subsequent MCP requests carry Mcp-Session-Id
    When I close the client session
    Then the last recorded MCP request method is "DELETE"

  Scenario: mid-stream disconnect is a transport failure
    Given a fake MCP server that disconnects mid-stream
    When I open a streamable session
    And I initialize through the client
    Then the last client error is tagged "transport"
