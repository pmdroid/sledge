package session

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pmdroid/mcp-loadtester/internal/fakes/mcphttp"
)

func TestJSONSession(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	c := New(Config{URL: srv.URL(), Headers: map[string]string{"X-Team": "platform"}})
	ctx := context.Background()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if c.ID() == "" {
		t.Fatal("empty session id")
	}
	if _, err := c.Call(ctx, "tools/list", map[string]any{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	res, err := c.Call(ctx, "tools/call", map[string]any{
		"name":      "search",
		"arguments": map[string]any{"query": "hello"},
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(res.Body, []byte("hello")) {
		t.Fatalf("body %s", res.Body)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if reqs[len(reqs)-1].Method != http.MethodDelete {
		t.Fatalf("last method %s", reqs[len(reqs)-1].Method)
	}
	for i, rec := range reqs {
		if rec.Header.Get("X-Team") != "platform" {
			t.Fatalf("request %d missing X-Team", i)
		}
	}
}

func TestSSESession(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeSSE})
	t.Cleanup(srv.Close)
	c := New(Config{URL: srv.URL()})
	res, err := c.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(res.ContentType), []byte("text/event-stream")) {
		t.Fatalf("ct %q", res.ContentType)
	}
	if !bytes.Contains(res.Result, []byte("protocolVersion")) {
		t.Fatalf("result %s", res.Result)
	}
}

func TestDisconnectTransport(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeSSEDisconnect})
	t.Cleanup(srv.Close)
	c := New(Config{URL: srv.URL()})
	_, err := c.Initialize(context.Background())
	if TagOf(err) != TagTransport {
		t.Fatalf("tag %q err %v", TagOf(err), err)
	}
}

func TestAuthStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(ts.Close)
	c := New(Config{URL: ts.URL})
	_, err := c.Initialize(context.Background())
	if TagOf(err) != TagAuth {
		t.Fatalf("tag %q err %v", TagOf(err), err)
	}
}

func TestIntendedLatency(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	c := New(Config{URL: srv.URL()})
	ctx := context.Background()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	intended := time.Now().Add(-40 * time.Millisecond)
	res, err := c.Call(ctx, "tools/list", map[string]any{}, intended)
	if err != nil {
		t.Fatal(err)
	}
	if res.IntendedLatency < 40*time.Millisecond {
		t.Fatalf("intended %s", res.IntendedLatency)
	}
	if res.ActualLatency > res.IntendedLatency {
		t.Fatalf("actual %s intended %s", res.ActualLatency, res.IntendedLatency)
	}
}
