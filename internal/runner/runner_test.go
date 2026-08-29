package runner

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pmdroid/mcp-loadtester/internal/fakes/mcphttp"
	"github.com/pmdroid/mcp-loadtester/internal/scenario"
)

func TestPerVUConcurrent(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	sc := mustLoad(t, yamlFor(srv.URL(), 10, "1s", "20ms", "per_vu", "vu", 0))
	var out bytes.Buffer
	sum, err := Run(context.Background(), Config{Scenario: sc, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if sum.VUs != 10 {
		t.Fatalf("vus %d", sum.VUs)
	}
	if sum.PeakSessions < 8 {
		t.Fatalf("peak %d, want ~10", sum.PeakSessions)
	}
	if srv.PeakSessions() < 8 {
		t.Fatalf("fake peak %d", srv.PeakSessions())
	}
	if sum.UniqueSessions < 8 {
		t.Fatalf("unique %d", sum.UniqueSessions)
	}
	if sum.HTTPClients != 10 || sum.HTTPPool != "vu" {
		t.Fatalf("pool %s clients %d", sum.HTTPPool, sum.HTTPClients)
	}
	if !strings.Contains(out.String(), "saturated_vus:") {
		t.Fatalf("summary %q", out.String())
	}
	if sum.Iterations == 0 {
		t.Fatal("no iterations")
	}
}

func TestPerIterationNewSession(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	sc := mustLoad(t, yamlFor(srv.URL(), 1, "2s", "0s", "per_iteration", "vu", 3))
	if sc.Workload.Session.Mode != "per_iteration" {
		t.Fatalf("mode %q", sc.Workload.Session.Mode)
	}
	if sc.Workload.Iterations != 3 {
		t.Fatalf("iterations %d", sc.Workload.Iterations)
	}
	sum, err := Run(context.Background(), Config{Scenario: sc, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.UniqueSessions < 3 {
		t.Fatalf("unique sessions %d, want >= 3 (ids=%v fake=%d)", sum.UniqueSessions, sum.SessionIDs, srv.UniqueSessions())
	}
	if srv.UniqueSessions() < 3 {
		t.Fatalf("fake unique %d", srv.UniqueSessions())
	}
	if sum.Iterations < 3 {
		t.Fatalf("iters %d", sum.Iterations)
	}
}

func TestSharedPool(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	sc := mustLoad(t, yamlFor(srv.URL(), 5, "400ms", "20ms", "per_vu", "shared", 0))
	sum, err := Run(context.Background(), Config{Scenario: sc, SharedPool: true, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.HTTPClients != 1 || sum.HTTPPool != "shared" {
		t.Fatalf("pool %s clients %d", sum.HTTPPool, sum.HTTPClients)
	}
	reqs := len(srv.Requests())
	if reqs < 10 {
		t.Fatalf("requests %d", reqs)
	}
	if srv.NewConns() >= reqs {
		t.Fatalf("conns %d requests %d (no reuse)", srv.NewConns(), reqs)
	}
}

func TestRampAndThink(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	y := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
workload:
  model: closed
  vus: 2
  duration: 400ms
  ramp_up: 80ms
  think_time: 50ms
  session:
    mode: per_vu
steps:
  - tools/list: {}
`, srv.URL())
	sc := mustLoad(t, y)
	sum, err := Run(context.Background(), Config{Scenario: sc, Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.SaturatedVUs != 0 {
		t.Fatalf("saturated %d with think_time", sum.SaturatedVUs)
	}
	if sum.VUs != 2 {
		t.Fatalf("vus %d", sum.VUs)
	}
}

func yamlFor(url string, vus int, dur, think, mode, pool string, iters int) string {
	iterLine := ""
	if iters > 0 {
		iterLine = fmt.Sprintf("  iterations: %d\n", iters)
	}
	return fmt.Sprintf(`version: 1
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
`, url, pool, vus, dur, think, iterLine, mode)
}

func mustLoad(t *testing.T, y string) *scenario.Scenario {
	t.Helper()
	sc, err := scenario.Load(strings.NewReader(y), scenario.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestMissingScenario(t *testing.T) {
	_, err := Run(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVUInterp(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	y := fmt.Sprintf(`version: 1
target:
  url: %s
  transport: streamable-http
  headers:
    X-VU: "${vu.id}"
workload:
  model: closed
  vus: 1
  duration: 200ms
  iterations: 1
  session:
    mode: per_vu
steps:
  - tools/call:
      name: echo
      arguments: { id: "${vu.id}" }
`, srv.URL())
	sc := mustLoad(t, y)
	if _, err := Run(context.Background(), Config{Scenario: sc, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, rec := range srv.Requests() {
		if rec.Header.Get("X-VU") == "0" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing X-VU header")
	}
}

func TestDurationOverrideWindow(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	sc := mustLoad(t, yamlFor(srv.URL(), 1, "150ms", "0s", "per_vu", "vu", 0))
	start := time.Now()
	if _, err := Run(context.Background(), Config{Scenario: sc, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond {
		t.Fatalf("elapsed %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("too long %s", elapsed)
	}
}
