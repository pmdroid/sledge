package metrics

import (
	"testing"
	"time"
)

func TestEvaluatePassFail(t *testing.T) {
	snap := Snapshot{
		ErrorRate: 0.0,
		P95:       10 * time.Millisecond,
		Failures:  Failures{Auth: 0},
	}
	rs, err := Evaluate(map[string]string{
		"error_rate":  "< 0.01",
		"p95_latency": "< 800ms",
		"auth_errors": "== 0",
	}, snap)
	if err != nil {
		t.Fatal(err)
	}
	if Failed(rs) {
		t.Fatalf("%+v", rs)
	}
	snap.P95 = time.Second
	rs, err = Evaluate(map[string]string{"p95_latency": "< 1ms"}, snap)
	if err != nil {
		t.Fatal(err)
	}
	if !Failed(rs) {
		t.Fatal("expected fail")
	}
}

func TestUnknownThreshold(t *testing.T) {
	_, err := Evaluate(map[string]string{"foo": "< 1"}, Snapshot{})
	if err == nil {
		t.Fatal("want error")
	}
}
