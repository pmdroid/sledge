Feature: Closed-model VU runner

  Scenario: 10 VUs keep concurrent sessions
    Given a fake MCP server using JSON responses
    And a closed scenario with 10 VUs for 5s think_time 20ms session mode per_vu
    When I run the scenario
    Then peak concurrent sessions is about 10
    And the run summary shows 10 VUs
    And saturated VUs are reported
    And the HTTP pool is "vu"

  Scenario: per_iteration new session each loop
    Given a fake MCP server using JSON responses
    And a closed scenario with 1 VU for 1s session mode per_iteration and 4 iterations
    When I run the scenario
    Then more than 1 unique session id was used
    And unique session ids equal the iteration count

  Scenario: shared HTTP pool reuses connections
    Given a fake MCP server using JSON responses
    And a closed scenario with 5 VUs for 1s with shared HTTP pool
    When I run the scenario
    Then the runner used 1 HTTP client
    And MCP connections are fewer than MCP requests
    And the HTTP pool is "shared"
    And saturated VUs are reported
