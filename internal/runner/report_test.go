package runner

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/pmdroid/sledge/internal/metrics"
	"github.com/pmdroid/sledge/internal/termstyle"
)

func TestWriteTextPlain(t *testing.T) {
	var buf bytes.Buffer
	sum := sampleSummary()
	sum.WriteText(&buf, termstyle.New(&buf, termstyle.Never))
	s := buf.String()
	for _, want := range []string{"closed model", "vus: 2", "p95_latency:", "throughput_rps:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
	if strings.Contains(s, "\033[") {
		t.Fatalf("plain text should not contain ansi: %q", s)
	}
}

func TestWriteTextStyled(t *testing.T) {
	var buf bytes.Buffer
	sum := sampleSummary()
	sum.WriteText(&buf, termstyle.New(&buf, termstyle.Always))
	s := buf.String()
	if !strings.Contains(s, "\033[") {
		t.Fatalf("expected ansi codes: %q", s)
	}
	for _, want := range []string{"SLEDGE", "vus:", "p95_latency:", "throughput_rps:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
}

func sampleSummary() *Summary {
	return &Summary{
		VUs:           2,
		Duration:      2 * time.Second,
		Iterations:    4,
		ThroughputRPS: 1.5,
		Errors:        0,
		P95Latency:    12 * time.Millisecond,
		HTTPPool:      "per_vu",
		HTTPClients:   2,
		Thresholds: []metrics.Result{
			{Name: "error_rate", Expr: "< 0.01", Actual: "0", OK: true},
		},
	}
}
