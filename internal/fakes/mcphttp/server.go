package mcphttp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

type Mode int

const (
	ModeJSON Mode = iota
	ModeSSE
	ModeSSESlow
	ModeSSEDisconnect
)

type Options struct {
	Mode      Mode
	SlowDelay time.Duration
}

type Recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type Server struct {
	http     *httptest.Server
	mode     Mode
	slow     time.Duration
	mu       sync.Mutex
	reqs     []Recorded
	sessions map[string]struct{}
}

func New(opts Options) *Server {
	s := &Server{
		mode:     opts.Mode,
		slow:     opts.SlowDelay,
		sessions: map[string]struct{}{},
	}
	if s.slow == 0 {
		s.slow = 20 * time.Millisecond
	}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *Server) URL() string {
	return s.http.URL + "/mcp"
}

func (s *Server) Close() {
	s.http.Close()
}

func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recorded, len(s.reqs))
	copy(out, s.reqs)
	return out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, Recorded{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Body:   body,
	})
	s.mu.Unlock()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}

	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	method, _ := msg["method"].(string)
	id := msg["id"]
	sid := r.Header.Get("Mcp-Session-Id")

	if method != "initialize" {
		s.mu.Lock()
		_, ok := s.sessions[sid]
		s.mu.Unlock()
		if sid == "" || !ok {
			http.Error(w, "missing or unknown session", http.StatusBadRequest)
			return
		}
	}

	switch method {
	case "initialize":
		sid = newID()
		s.mu.Lock()
		s.sessions[sid] = struct{}{}
		s.mu.Unlock()
		s.writeRPC(w, sid, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.0.0"},
			},
		})
	case "tools/list":
		s.writeRPC(w, sid, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"tools": []any{
					map[string]any{
						"name":        "search",
						"description": "search",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"query": map[string]any{"type": "string"},
							},
						},
					},
					map[string]any{
						"name":        "echo",
						"description": "echo",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			},
		})
	case "tools/call":
		params, _ := msg["params"].(map[string]any)
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)
		s.writeRPC(w, sid, callResult(id, name, args))
	default:
		s.writeRPC(w, sid, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": -32601, "message": "Method not found"},
		})
	}
}

func callResult(id any, name string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	var text string
	ok := true
	switch name {
	case "search":
		q, _ := args["query"].(string)
		text = q
	case "echo":
		raw, _ := json.Marshal(args)
		text = string(raw)
	default:
		ok = false
		text = "unknown tool"
	}
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
	}
	if !ok {
		result["isError"] = true
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}

func (s *Server) writeRPC(w http.ResponseWriter, sid string, msg any) {
	if sid != "" {
		w.Header().Set("Mcp-Session-Id", sid)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	switch s.mode {
	case ModeJSON:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		_, _ = w.Write([]byte("\n"))
	case ModeSSE:
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw)
		flush(w)
	case ModeSSESlow:
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message\n")
		flush(w)
		time.Sleep(s.slow)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flush(w)
	case ModeSSEDisconnect:
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\"")
		flush(w)
		if hj, ok := w.(http.Hijacker); ok {
			c, _, err := hj.Hijack()
			if err == nil {
				_ = c.Close()
			}
		}
	}
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
