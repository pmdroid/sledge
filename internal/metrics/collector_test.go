package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/pmdroid/mcp-loadtester/internal/session"
)

func TestCollectorTagsAndP95(t *testing.T) {
	c := NewCollector()
	c.RecordSetup(2*time.Millisecond, 2*time.Millisecond, nil)
	for i := 0; i < 20; i++ {
		c.RecordOp("tools/list", "", 5*time.Millisecond, 4*time.Millisecond, nil)
	}
	c.RecordOp("tools/call", "search", 8*time.Millisecond, 7*time.Millisecond, nil)
	c.RecordOp("tools/call", "search", 8*time.Millisecond, 7*time.Millisecond, &session.Error{Tag: session.TagAuth, Err: fmt.Errorf("http 401")})
	c.RecordOp("tools/list", "", 5*time.Millisecond, 4*time.Millisecond, &session.Error{Tag: session.TagProtocol, Err: fmt.Errorf("rpc")})
	snap := c.Snapshot()
	if snap.Failures.Auth != 1 {
		t.Fatalf("auth %d", snap.Failures.Auth)
	}
	if snap.Failures.Protocol != 1 {
		t.Fatalf("protocol %d", snap.Failures.Protocol)
	}
	if snap.Failures.Transport != 0 || snap.Failures.Assertion != 0 {
		t.Fatalf("%+v", snap.Failures)
	}
	if snap.Operations["tools/list"].Intended.Count < 20 {
		t.Fatalf("list count %d", snap.Operations["tools/list"].Intended.Count)
	}
	if snap.Tools["search"].Intended.Count != 2 {
		t.Fatalf("search %d", snap.Tools["search"].Intended.Count)
	}
	if snap.Setup.Intended.Count != 1 {
		t.Fatalf("setup %d", snap.Setup.Intended.Count)
	}
	if snap.P95 < 4*time.Millisecond {
		t.Fatalf("p95 %s", snap.P95)
	}
	if snap.ErrorRate <= 0 {
		t.Fatalf("error_rate %f", snap.ErrorRate)
	}
}
