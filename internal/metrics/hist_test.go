package metrics

import (
	"testing"
	"time"
)

func TestHistPercentiles(t *testing.T) {
	h := NewHist()
	for i := 1; i <= 100; i++ {
		h.Record(time.Duration(i) * time.Millisecond)
	}
	p := h.Snapshot()
	if p.Count != 100 {
		t.Fatalf("count %d", p.Count)
	}
	if p.P50 < 40*time.Millisecond || p.P50 > 60*time.Millisecond {
		t.Fatalf("p50 %s", p.P50)
	}
	if p.P95 < 90*time.Millisecond || p.P95 > 100*time.Millisecond {
		t.Fatalf("p95 %s", p.P95)
	}
	if p.P99 < 95*time.Millisecond || p.P99 > 100*time.Millisecond {
		t.Fatalf("p99 %s", p.P99)
	}
}

func TestEmptyHist(t *testing.T) {
	p := NewHist().Snapshot()
	if p.Count != 0 || p.P95 != 0 {
		t.Fatalf("%+v", p)
	}
}
