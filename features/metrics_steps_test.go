package features_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/pmdroid/sledge/internal/fakes/mcphttp"
)

func initMetrics(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a fake MCP server using JSON responses delayed (\S+)$`, w.startMCPDelayed)
	sc.Step(`^a closed scenario with (\d+) VU for (\S+) with p95_latency "([^"]*)"$`, w.writeP95Scenario)
	sc.Step(`^a closed scenario with (\d+) VU for (\S+) with no auth$`, w.writeNoAuthScenario)
	sc.Step(`^a closed scenario with static bearer "([^"]*)" and (\d+) VU for (\S+)$`, w.writeBearerScenario)
	sc.Step(`^the run exits (\d+)$`, w.runExits)
	sc.Step(`^the JSON report has intended and actual latency$`, w.jsonHasLatencies)
	sc.Step(`^the text report mentions closed-model understated tails$`, w.textHasClosedNote)
	sc.Step(`^auth failures are greater than (\d+)$`, w.authFailuresGT)
	sc.Step(`^protocol failures are (\d+)$`, w.protocolFailures)
	sc.Step(`^the JSON report does not contain "([^"]*)"$`, w.jsonOmits)
	sc.Step(`^intended and uncorrected latency are both present$`, w.jsonHasLatencies)
}

func (w *world) startMCPDelayed(d string) error {
	delay, err := time.ParseDuration(d)
	if err != nil {
		return err
	}
	return w.startMCP(mcphttp.Options{Mode: mcphttp.ModeJSON, Delay: delay})
}

func (w *world) writeP95Scenario(vus int, dur, expr string) error {
	return w.writeExtraYAML(vus, dur, "", "", fmt.Sprintf("thresholds:\n  p95_latency: %q\n", expr))
}

func (w *world) writeNoAuthScenario(vus int, dur string) error {
	return w.writeExtraYAML(vus, dur, "", "", "")
}

func (w *world) writeBearerScenario(tok string, vus int, dur string) error {
	headers := fmt.Sprintf("  headers:\n    Authorization: Bearer %s\n", tok)
	return w.writeExtraYAML(vus, dur, headers, tok, "")
}

func (w *world) writeExtraYAML(vus int, dur, headers, token, extra string) error {
	if w.mcp == nil {
		return fmt.Errorf("no mcp server")
	}
	dir, err := os.MkdirTemp("", "sledge-metrics-")
	if err != nil {
		return err
	}
	w.runDir = dir
	body := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
%sworkload:
  model: closed
  vus: %d
  duration: %s
  ramp_up: 0s
  think_time: 0s
  session:
    mode: per_vu
steps:
  - tools/list: {}
%s`, w.mcp.URL(), headers, vus, dur, extra)
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	w.scenPath = path
	w.secretTok = token
	return nil
}

func (w *world) runExits(code int) error {
	if w.runCode != code {
		return fmt.Errorf("exit %d, want %d err=%v failed=%v", w.runCode, code, w.runErr, w.runSum != nil && w.runSum.Failed)
	}
	return nil
}

func (w *world) jsonHasLatencies() error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	js := string(w.runSum.JSON)
	if !strings.Contains(js, `"intended"`) || !strings.Contains(js, `"actual"`) {
		return fmt.Errorf("json missing intended/actual: %s", js)
	}
	if !strings.Contains(js, `"p95"`) {
		return fmt.Errorf("json missing p95: %s", js)
	}
	return nil
}

func (w *world) textHasClosedNote() error {
	if w.runOut == nil {
		return fmt.Errorf("no output")
	}
	s := w.runOut.String()
	if !strings.Contains(s, "understates tails") || !strings.Contains(s, "actual-send") {
		return fmt.Errorf("text %q", s)
	}
	return nil
}

func (w *world) authFailuresGT(n int) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.Failures.Auth <= int64(n) {
		return fmt.Errorf("auth %d, want > %d protocol=%d", w.runSum.Failures.Auth, n, w.runSum.Failures.Protocol)
	}
	return nil
}

func (w *world) protocolFailures(n int) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.Failures.Protocol != int64(n) {
		return fmt.Errorf("protocol %d, want %d auth=%d", w.runSum.Failures.Protocol, n, w.runSum.Failures.Auth)
	}
	return nil
}

func (w *world) jsonOmits(s string) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if strings.Contains(string(w.runSum.JSON), s) {
		return fmt.Errorf("json leaked %q", s)
	}
	if w.runOut != nil && strings.Contains(w.runOut.String(), s) {
		return fmt.Errorf("text leaked %q", s)
	}
	return nil
}
