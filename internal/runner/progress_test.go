package runner

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pmdroid/sledge/internal/fakes/mcphttp"
	"github.com/pmdroid/sledge/internal/metrics"
)

func TestWriteProgressLine(t *testing.T) {
	var buf bytes.Buffer
	iters := atomic.Int64{}
	iters.Store(3)
	coll := metrics.NewCollector()
	coll.RecordOp("tools/list", "", 10*time.Millisecond, 10*time.Millisecond, nil)
	writeProgressLine(&buf, time.Now().Add(-2*time.Second), &iters, coll)
	if !strings.Contains(buf.String(), "iters=3") {
		t.Fatalf("got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "ops=1") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestProgressDuringRun(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON, Delay: 30 * time.Millisecond})
	t.Cleanup(srv.Close)
	sc := mustLoad(t, yamlFor(srv.URL(), 2, "500ms", "0s", "per_vu", "vu", 0))
	var errBuf bytes.Buffer
	_, err := Run(context.Background(), Config{
		Scenario: sc,
		Stdout:   &bytes.Buffer{},
		Stderr:   &errBuf,
		Progress: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "iters=") || !strings.Contains(out, "rps=") {
		t.Fatalf("progress stderr %q", out)
	}
}
