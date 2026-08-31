package runner

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/pmdroid/sledge/internal/metrics"
)

func runProgress(w io.Writer, start time.Time, iters *atomic.Int64, coll *metrics.Collector, done <-chan struct{}) {
	if w == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			writeProgressLine(w, start, iters, coll)
		}
	}
}

func writeProgressLine(w io.Writer, start time.Time, iters *atomic.Int64, coll *metrics.Collector) {
	elapsed := time.Since(start)
	snap := coll.Snapshot()
	ops := snap.Ops
	var rps float64
	if elapsed > 0 {
		rps = float64(ops) / elapsed.Seconds()
	}
	iter := int64(0)
	if iters != nil {
		iter = iters.Load()
	}
	errs := snap.Failures.Total()
	fmt.Fprintf(w, "\r[%s] iters=%d ops=%d errs=%d (%.1f%%) rps=%.1f p95=%s   ",
		elapsed.Truncate(time.Second),
		iter,
		ops,
		errs,
		snap.ErrorRate*100,
		rps,
		snap.P95.Truncate(time.Millisecond),
	)
}
