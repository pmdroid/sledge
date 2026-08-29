package metrics

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const (
	histMin int64 = 1
	histMax int64 = int64(time.Hour)
	histSig       = 3
)

type Percentiles struct {
	Count int64
	P50   time.Duration
	P90   time.Duration
	P95   time.Duration
	P99   time.Duration
}

func (p Percentiles) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Count int64  `json:"count"`
		P50   string `json:"p50"`
		P90   string `json:"p90"`
		P95   string `json:"p95"`
		P99   string `json:"p99"`
	}{
		Count: p.Count,
		P50:   p.P50.String(),
		P90:   p.P90.String(),
		P95:   p.P95.String(),
		P99:   p.P99.String(),
	})
}

type Hist struct {
	mu sync.Mutex
	h  *hdrhistogram.Histogram
	n  int64
}

func NewHist() *Hist {
	return &Hist{h: hdrhistogram.New(histMin, histMax, histSig)}
}

func (h *Hist) Record(d time.Duration) {
	if h == nil {
		return
	}
	v := d.Nanoseconds()
	if v < histMin {
		v = histMin
	}
	if v > histMax {
		v = histMax
	}
	h.mu.Lock()
	_ = h.h.RecordValue(v)
	h.n++
	h.mu.Unlock()
}

func (h *Hist) Snapshot() Percentiles {
	if h == nil {
		return Percentiles{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n == 0 {
		return Percentiles{}
	}
	return Percentiles{
		Count: h.n,
		P50:   time.Duration(h.h.ValueAtQuantile(50)),
		P90:   time.Duration(h.h.ValueAtQuantile(90)),
		P95:   time.Duration(h.h.ValueAtQuantile(95)),
		P99:   time.Duration(h.h.ValueAtQuantile(99)),
	}
}

type Pair struct {
	Intended Percentiles `json:"intended"`
	Actual   Percentiles `json:"actual"`
}

type pairHist struct {
	intended *Hist
	actual   *Hist
}

func newPairHist() *pairHist {
	return &pairHist{intended: NewHist(), actual: NewHist()}
}

func (p *pairHist) record(intended, actual time.Duration) {
	if p == nil {
		return
	}
	p.intended.Record(intended)
	p.actual.Record(actual)
}

func (p *pairHist) snapshot() Pair {
	if p == nil {
		return Pair{}
	}
	return Pair{Intended: p.intended.Snapshot(), Actual: p.actual.Snapshot()}
}
