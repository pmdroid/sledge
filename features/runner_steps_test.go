package features_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
	"github.com/pmdroid/mcp-loadtester/internal/runner"
	"github.com/pmdroid/mcp-loadtester/internal/scenario"
)

func initRunner(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a closed scenario with (\d+) VUs for (\S+) session mode (per_vu|per_iteration)$`, w.writeClosed)
	sc.Step(`^a closed scenario with (\d+) VUs for (\S+) think_time (\S+) session mode (per_vu|per_iteration)$`, w.writeClosedThink)
	sc.Step(`^a closed scenario with (\d+) VU for (\S+) session mode (per_vu|per_iteration) and (\d+) iterations$`, w.writeClosedIters)
	sc.Step(`^a closed scenario with (\d+) VUs for (\S+) with shared HTTP pool$`, w.writeShared)
	sc.Step(`^I run the scenario$`, w.runScenario)
	sc.Step(`^peak concurrent sessions is about (\d+)$`, w.peakAbout)
	sc.Step(`^the run summary shows (\d+) VUs$`, w.summaryVUs)
	sc.Step(`^saturated VUs are reported$`, w.saturatedReported)
	sc.Step(`^the HTTP pool is "([^"]*)"$`, w.httpPoolIs)
	sc.Step(`^more than (\d+) unique session id was used$`, w.uniqueMoreThan)
	sc.Step(`^unique session ids equal the iteration count$`, w.uniqueEqualsIters)
	sc.Step(`^the runner used (\d+) HTTP client$`, w.httpClients)
	sc.Step(`^MCP connections are fewer than MCP requests$`, w.connsReused)
}

func (w *world) writeClosed(vus int, dur, mode string) error {
	return w.writeScenarioYAML(vus, dur, "0s", mode, "vu", 0)
}

func (w *world) writeClosedThink(vus int, dur, think, mode string) error {
	return w.writeScenarioYAML(vus, dur, think, mode, "vu", 0)
}

func (w *world) writeClosedIters(vus int, dur, mode string, iters int) error {
	return w.writeScenarioYAML(vus, dur, "0s", mode, "vu", iters)
}

func (w *world) writeShared(vus int, dur string) error {
	return w.writeScenarioYAML(vus, dur, "20ms", "per_vu", "shared", 0)
}

func (w *world) writeScenarioYAML(vus int, dur, think, mode, pool string, iters int) error {
	if w.mcp == nil {
		return fmt.Errorf("no mcp server")
	}
	dir, err := os.MkdirTemp("", "mcpload-run-")
	if err != nil {
		return err
	}
	w.runDir = dir
	iterLine := ""
	if iters > 0 {
		iterLine = fmt.Sprintf("  iterations: %d\n", iters)
	}
	body := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
http:
  pool: %s
workload:
  model: closed
  vus: %d
  duration: %s
  ramp_up: 0s
  think_time: %s
%s  session:
    mode: %s
steps:
  - tools/list: {}
`, w.mcp.URL(), pool, vus, dur, think, iterLine, mode)
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	w.scenPath = path
	return nil
}

func (w *world) runScenario() error {
	if w.scenPath == "" {
		return fmt.Errorf("no scenario")
	}
	sc, err := scenario.LoadFile(w.scenPath, scenario.Options{})
	if err != nil {
		return err
	}
	w.runOut = &bytes.Buffer{}
	sum, err := runner.Run(context.Background(), runner.Config{
		Scenario:   sc,
		SharedPool: sc.HTTP.Pool == "shared",
		Stdout:     w.runOut,
	})
	w.runSum = sum
	w.runErr = err
	w.runCode = 0
	if err != nil {
		w.runCode = 3
		return err
	}
	if sum != nil && sum.Failed {
		w.runCode = 1
	}
	return nil
}

func (w *world) peakAbout(n int) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	lo := n - 2
	if lo < 1 {
		lo = 1
	}
	if w.runSum.PeakSessions < lo || w.runSum.PeakSessions > n+2 {
		return fmt.Errorf("peak_sessions %d, want about %d", w.runSum.PeakSessions, n)
	}
	if w.mcp.PeakSessions() < lo {
		return fmt.Errorf("fake peak %d, want about %d", w.mcp.PeakSessions(), n)
	}
	return nil
}

func (w *world) summaryVUs(n int) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.VUs != n {
		return fmt.Errorf("vus %d, want %d", w.runSum.VUs, n)
	}
	if !strings.Contains(w.runOut.String(), fmt.Sprintf("vus: %d", n)) {
		return fmt.Errorf("summary missing vus: %q", w.runOut.String())
	}
	return nil
}

func (w *world) saturatedReported() error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if !strings.Contains(w.runOut.String(), "saturated_vus:") {
		return fmt.Errorf("summary %q", w.runOut.String())
	}
	if w.runSum.SaturatedVUs < 0 {
		return fmt.Errorf("saturated %d", w.runSum.SaturatedVUs)
	}
	return nil
}

func (w *world) httpPoolIs(pool string) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.HTTPPool != pool {
		return fmt.Errorf("pool %q, want %q", w.runSum.HTTPPool, pool)
	}
	return nil
}

func (w *world) uniqueMoreThan(n int) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.UniqueSessions <= n {
		return fmt.Errorf("unique %d, want > %d", w.runSum.UniqueSessions, n)
	}
	return nil
}

func (w *world) uniqueEqualsIters() error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.UniqueSessions != int(w.runSum.Iterations) {
		return fmt.Errorf("unique %d iters %d", w.runSum.UniqueSessions, w.runSum.Iterations)
	}
	return nil
}

func (w *world) httpClients(n int) error {
	if w.runSum == nil {
		return fmt.Errorf("no run")
	}
	if w.runSum.HTTPClients != n {
		return fmt.Errorf("clients %d, want %d", w.runSum.HTTPClients, n)
	}
	return nil
}

func (w *world) connsReused() error {
	if w.mcp == nil {
		return fmt.Errorf("no mcp")
	}
	reqs := len(w.mcp.Requests())
	conns := w.mcp.NewConns()
	if reqs == 0 {
		return fmt.Errorf("no requests")
	}
	if conns >= reqs {
		return fmt.Errorf("conns %d requests %d", conns, reqs)
	}
	return nil
}
