package runner

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/pmdroid/sledge/internal/metrics"
	"github.com/pmdroid/sledge/internal/termstyle"
)

func runProgress(w io.Writer, start time.Time, iters *atomic.Int64, coll *metrics.Collector, done <-chan struct{}, sty *termstyle.Theme) {
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
			writeProgressLine(w, start, iters, coll, sty)
		}
	}
}

func writeProgressLine(w io.Writer, start time.Time, iters *atomic.Int64, coll *metrics.Collector, sty *termstyle.Theme) {
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
	el := elapsed.Truncate(time.Second).String()
	p95 := snap.P95.Truncate(time.Millisecond).String()
	if sty != nil && sty.On() {
		errPct := fmt.Sprintf("(%.1f%%)", snap.ErrorRate*100)
		errCount := fmt.Sprintf("%d", errs)
		if errs > 0 {
			errCount = sty.Bad(errCount)
			errPct = sty.Bad(errPct)
		}
		fmt.Fprintf(w, "\r%s %s=%d %s=%d %s=%s %s %s=%.1f %s=%s   ",
			sty.Dim("["+el+"]"),
			sty.Label("iters"), iter,
			sty.Label("ops"), ops,
			sty.Label("errs"), errCount,
			errPct,
			sty.Label("rps"), rps,
			sty.Accent("p95"), p95,
		)
		return
	}
	fmt.Fprintf(w, "\r[%s] iters=%d ops=%d errs=%d (%.1f%%) rps=%.1f p95=%s   ",
		el,
		iter,
		ops,
		errs,
		snap.ErrorRate*100,
		rps,
		p95,
	)
}
