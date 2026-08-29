package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/pmdroid/mcp-loadtester/internal/metrics"
	"github.com/pmdroid/mcp-loadtester/internal/redact"
)

const closedModelNote = "closed-model intended-send latency understates tails when the server stalls; uncorrected actual-send latency is also reported"

type jsonReport struct {
	VUs            int                     `json:"vus"`
	Duration       string                  `json:"duration"`
	Iterations     int64                   `json:"iterations"`
	ThroughputRPS  float64                 `json:"throughput_rps"`
	ErrorRate      float64                 `json:"error_rate"`
	PeakSessions   int                     `json:"peak_sessions"`
	SaturatedVUs   int                     `json:"saturated_vus"`
	UniqueSessions int                     `json:"unique_sessions"`
	HTTPPool       string                  `json:"http_pool"`
	HTTPClients    int                     `json:"http_clients"`
	SetupCount     int64                   `json:"setup_count"`
	Errors         int64                   `json:"errors"`
	Failures       metrics.Failures        `json:"failures"`
	P95Latency     string                  `json:"p95_latency"`
	Setup          metrics.Pair            `json:"setup"`
	Operations     map[string]metrics.Pair `json:"operations"`
	Tools          map[string]metrics.Pair `json:"tools"`
	Thresholds     []metrics.Result        `json:"thresholds"`
	Failed         bool                    `json:"failed"`
	Note           string                  `json:"note"`
}

func (s *Summary) report() jsonReport {
	if s == nil {
		return jsonReport{}
	}
	return jsonReport{
		VUs:            s.VUs,
		Duration:       s.Duration.String(),
		Iterations:     s.Iterations,
		ThroughputRPS:  s.ThroughputRPS,
		ErrorRate:      s.ErrorRate,
		PeakSessions:   s.PeakSessions,
		SaturatedVUs:   s.SaturatedVUs,
		UniqueSessions: s.UniqueSessions,
		HTTPPool:       s.HTTPPool,
		HTTPClients:    s.HTTPClients,
		SetupCount:     s.SetupCount,
		Errors:         s.Errors,
		Failures:       s.Failures,
		P95Latency:     s.P95Latency.String(),
		Setup:          s.Setup,
		Operations:     s.Operations,
		Tools:          s.Tools,
		Thresholds:     s.Thresholds,
		Failed:         s.Failed,
		Note:           closedModelNote,
	}
}

func (s *Summary) WriteText(w io.Writer) {
	if s == nil || w == nil {
		return
	}
	fmt.Fprintf(w, "closed model\n")
	fmt.Fprintf(w, "vus: %d\n", s.VUs)
	fmt.Fprintf(w, "duration: %s\n", s.Duration)
	fmt.Fprintf(w, "iterations: %d\n", s.Iterations)
	fmt.Fprintf(w, "throughput_rps: %.4f\n", s.ThroughputRPS)
	fmt.Fprintf(w, "peak_sessions: %d\n", s.PeakSessions)
	fmt.Fprintf(w, "saturated_vus: %d\n", s.SaturatedVUs)
	fmt.Fprintf(w, "unique_sessions: %d\n", s.UniqueSessions)
	fmt.Fprintf(w, "http_pool: %s\n", s.HTTPPool)
	fmt.Fprintf(w, "http_clients: %d\n", s.HTTPClients)
	fmt.Fprintf(w, "setup: %d\n", s.SetupCount)
	fmt.Fprintf(w, "errors: %d\n", s.Errors)
	fmt.Fprintf(w, "error_rate: %.6f\n", s.ErrorRate)
	fmt.Fprintf(w, "failures: transport=%d auth=%d protocol=%d assertion=%d\n", s.Failures.Transport, s.Failures.Auth, s.Failures.Protocol, s.Failures.Assertion)
	fmt.Fprintf(w, "p95_latency: %s\n", s.P95Latency)
	writePair(w, "setup", s.Setup)
	writePairs(w, "operations", s.Operations)
	writePairs(w, "tools", s.Tools)
	if len(s.Thresholds) > 0 {
		fmt.Fprintf(w, "thresholds:\n")
		for _, t := range s.Thresholds {
			st := "ok"
			if !t.OK {
				st = "FAIL"
			}
			fmt.Fprintf(w, "  %s: %s actual=%s %s\n", t.Name, t.Expr, t.Actual, st)
		}
	}
	fmt.Fprintf(w, "note: %s\n", closedModelNote)
}

func writePairs(w io.Writer, title string, m map[string]metrics.Pair) {
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "%s:\n", title)
	for _, k := range keys {
		writePair(w, "  "+k, m[k])
	}
}

func writePair(w io.Writer, name string, p metrics.Pair) {
	fmt.Fprintf(w, "%s count=%d\n", name, p.Intended.Count)
	fmt.Fprintf(w, "  intended p50=%s p90=%s p95=%s p99=%s\n", p.Intended.P50, p.Intended.P90, p.Intended.P95, p.Intended.P99)
	fmt.Fprintf(w, "  actual p50=%s p90=%s p95=%s p99=%s\n", p.Actual.P50, p.Actual.P90, p.Actual.P95, p.Actual.P99)
}

func (s *Summary) marshalJSON(log *redact.Logger) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	raw, err := json.MarshalIndent(s.report(), "", "  ")
	if err != nil {
		return nil, err
	}
	if log != nil {
		raw = []byte(log.Scrub(string(raw)))
	}
	s.JSON = raw
	return raw, nil
}

func (s *Summary) WriteJSON(w io.Writer, log *redact.Logger) error {
	raw, err := s.marshalJSON(log)
	if err != nil {
		return err
	}
	if w == nil {
		return nil
	}
	_, err = w.Write(raw)
	return err
}

func writeOutFile(path string, body []byte) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, body, 0o600)
}
