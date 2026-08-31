package session

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pmdroid/sledge/internal/fakes/mcphttp"
)

func TestPickVersion(t *testing.T) {
	if got := PickVersion([]string{"2025-06-18", "2025-11-25", "2026-07-28"}); got != Proto20260728 {
		t.Fatalf("got %q", got)
	}
	if got := PickVersion([]string{"2025-03-26", "2025-11-25"}); got != Proto20251125 {
		t.Fatalf("got %q", got)
	}
	if got := PickVersion([]string{"2025-03-26", "2025-06-18"}); got != "" {
		t.Fatalf("got %q", got)
	}
}

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
	if c.Protocol() != Proto20260728 {
		t.Fatalf("protocol %q", c.Protocol())
	}
	if reqs := srv.Requests(); len(reqs) == 0 || reqs[0].Header.Get("MCP-Protocol-Version") != Proto20260728 {
		t.Fatalf("initialize header")
	}
	if srv.Requests()[0].Header.Get("Mcp-Method") != "initialize" {
		t.Fatalf("Mcp-Method %q", srv.Requests()[0].Header.Get("Mcp-Method"))
	}
	var initMsg map[string]any
	if err := json.Unmarshal(srv.Requests()[0].Body, &initMsg); err != nil {
		t.Fatal(err)
	}
	params, _ := initMsg["params"].(map[string]any)
	meta, _ := params["_meta"].(map[string]any)
	if meta["io.modelcontextprotocol/protocolVersion"] != Proto20260728 {
		t.Fatalf("meta %#v", meta)
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

func TestAuthInject(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	srv.RequireToken("tok-1")
	c := New(Config{
		URL: srv.URL(),
		Auth: func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer tok-1")
			return nil
		},
	})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultUserAgentOverride(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	c := New(Config{URL: srv.URL(), Headers: map[string]string{"User-Agent": "custom/1"}})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Requests()[0].Header.Get("User-Agent"); got != "custom/1" {
		t.Fatalf("User-Agent %q", got)
	}
}

func TestDefaultUserAgent(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	c := New(Config{URL: srv.URL()})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Requests()[0].Header.Get("User-Agent"); got != UserAgent {
		t.Fatalf("User-Agent %q", got)
	}
}

func TestAuthInjectError(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	c := New(Config{
		URL: srv.URL(),
		Auth: func(context.Context, *http.Request) error {
			return context.DeadlineExceeded
		},
	})
	_, err := c.Initialize(context.Background())
	if TagOf(err) != TagAuth {
		t.Fatalf("tag %q err %v", TagOf(err), err)
	}
}

func TestStaticBearerHeader(t *testing.T) {
	srv := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(srv.Close)
	srv.RequireToken("static-token")
	c := New(Config{URL: srv.URL(), Headers: map[string]string{"Authorization": "Bearer static-token"}})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
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

func TestNegotiateOlderSupportedVersion(t *testing.T) {
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     int64          `json:"id"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		requested, _ := msg.Params["protocolVersion"].(string)
		seen = append(seen, requested)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "s1")
		if requested != Proto20251125 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"error": map[string]any{
					"code":    RPCUnsupportedVersion,
					"message": "Unsupported protocol version",
					"data":    map[string]any{"requested": requested, "supported": []string{Proto20251125, "2025-06-18"}},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"result":  map[string]any{"protocolVersion": Proto20251125, "capabilities": map[string]any{}},
		})
	}))
	t.Cleanup(ts.Close)
	c := New(Config{URL: ts.URL})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Protocol() != Proto20251125 {
		t.Fatalf("protocol %q", c.Protocol())
	}
	if len(seen) != 2 || seen[0] != Proto20260728 || seen[1] != Proto20251125 {
		t.Fatalf("seen %v", seen)
	}
}

func TestFallbackOnInvalidParams(t *testing.T) {
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     int64          `json:"id"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		requested, _ := msg.Params["protocolVersion"].(string)
		seen = append(seen, requested)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "s1")
		if requested == Proto20260728 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"error": map[string]any{
					"code":    -32602,
					"message": "missing or invalid _meta field \"io.modelcontextprotocol/protocolVersion\"",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"result":  map[string]any{"protocolVersion": Proto20251125, "capabilities": map[string]any{}},
		})
	}))
	t.Cleanup(ts.Close)
	c := New(Config{URL: ts.URL})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Protocol() != Proto20251125 {
		t.Fatalf("protocol %q", c.Protocol())
	}
	if len(seen) != 2 || seen[0] != Proto20260728 || seen[1] != Proto20251125 {
		t.Fatalf("seen %v", seen)
	}
}

func TestRejectUnknownProtocol(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"error": map[string]any{
				"code":    RPCUnsupportedVersion,
				"message": "Unsupported protocol version",
				"data":    map[string]any{"supported": []string{"2025-03-26"}},
			},
		})
	}))
	t.Cleanup(ts.Close)
	c := New(Config{URL: ts.URL})
	_, err := c.Initialize(context.Background())
	if TagOf(err) != TagProtocol {
		t.Fatalf("tag %q err %v", TagOf(err), err)
	}
}

func TestAcceptedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "s1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"protocolVersion": Proto20260728},
		})
	}))
	t.Cleanup(ts.Close)
	c := New(Config{URL: ts.URL})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
