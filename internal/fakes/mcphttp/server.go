package mcphttp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
	Delay     time.Duration
	Versions  []string
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
	delay    time.Duration
	mu       sync.Mutex
	reqs     []Recorded
	sessions map[string]struct{}
	seen     map[string]struct{}
	peak           int
	newConns       int
	authOK         func(string) bool
	requireHeaders map[string]string
	always401      bool
	wwwAuth        string
	versions       []string
}

func New(opts Options) *Server {
	s := &Server{
		mode:     opts.Mode,
		slow:     opts.SlowDelay,
		delay:    opts.Delay,
		sessions: map[string]struct{}{},
		seen:     map[string]struct{}{},
		versions: opts.Versions,
	}
	if s.slow == 0 {
		s.slow = 20 * time.Millisecond
	}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	s.http.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			s.mu.Lock()
			s.newConns++
			s.mu.Unlock()
		}
	}
	return s
}

func (s *Server) URL() string {
	return s.http.URL + "/mcp"
}

func (s *Server) Close() {
	s.http.Close()
}

func (s *Server) RequireToken(tok string) {
	s.RequireAccess(func(got string) bool { return got == tok })
}

func (s *Server) RequireAccess(fn func(string) bool) {
	s.mu.Lock()
	s.authOK = fn
	s.mu.Unlock()
}

func (s *Server) RequireHeader(key, val string) {
	s.mu.Lock()
	if s.requireHeaders == nil {
		s.requireHeaders = map[string]string{}
	}
	s.requireHeaders[key] = val
	s.mu.Unlock()
}

func (s *Server) AlwaysUnauthorized() {
	s.mu.Lock()
	s.always401 = true
	s.mu.Unlock()
}

func (s *Server) SetWWWAuthenticate(v string) {
	s.mu.Lock()
	s.wwwAuth = v
	s.mu.Unlock()
}

func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recorded, len(s.reqs))
	copy(out, s.reqs)
	return out
}

func (s *Server) PeakSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}

func (s *Server) UniqueSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func (s *Server) SessionIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.seen))
	for id := range s.seen {
		out = append(out, id)
	}
	return out
}

func (s *Server) NewConns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newConns
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

	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	always401 := s.always401
	authOK := s.authOK
	wwwAuth := s.wwwAuth
	reqHeaders := map[string]string{}
	for k, v := range s.requireHeaders {
		reqHeaders[k] = v
	}
	s.mu.Unlock()
	write401 := func() {
		if wwwAuth != "" {
			w.Header().Set("WWW-Authenticate", wwwAuth)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
	if always401 {
		write401()
		return
	}
	if authOK != nil {
		h := r.Header.Get("Authorization")
		tok := ""
		if strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimSpace(h[len("Bearer "):])
		}
		if tok == "" || !authOK(tok) {
			write401()
			return
		}
	}
	for k, v := range reqHeaders {
		if r.Header.Get(k) != v {
			write401()
			return
		}
	}
	if s.delay > 0 && r.Method != http.MethodDelete {
		time.Sleep(s.delay)
	}
	if r.Method == http.MethodDelete {
		sid := r.Header.Get("Mcp-Session-Id")
		s.mu.Lock()
		_, ok := s.sessions[sid]
		if ok {
			delete(s.sessions, sid)
		}
		s.mu.Unlock()
		if sid == "" || !ok {
			http.Error(w, "missing or unknown session", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	proto := requestProtocol(r, msg)

	if strings.HasPrefix(method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if method != "initialize" && method != "server/discover" && proto != "2026-07-28" {
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
		requested := ""
		if params, ok := msg["params"].(map[string]any); ok {
			requested, _ = params["protocolVersion"].(string)
		}
		if !s.supportedProtocol(requested) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]any{
					"code":    -32022,
					"message": "Unsupported protocol version",
					"data": map[string]any{
						"requested": requested,
						"supported": s.supportedList(),
					},
				},
			})
			return
		}
		sid = newID()
		s.mu.Lock()
		s.sessions[sid] = struct{}{}
		s.seen[sid] = struct{}{}
		if len(s.sessions) > s.peak {
			s.peak = len(s.sessions)
		}
		s.mu.Unlock()
		s.writeRPC(w, sid, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": requested,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.0.0"},
			},
		})
	case "server/discover":
		ver := proto
		if !s.supportedProtocol(ver) {
			ver = "2026-07-28"
			if !s.supportedProtocol(ver) {
				ver = s.supportedList()[0]
			}
		}
		s.writeRPC(w, "", map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": ver,
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

func (s *Server) supportedList() []string {
	if len(s.versions) > 0 {
		return s.versions
	}
	return []string{"2026-07-28", "2025-11-25"}
}

func (s *Server) supportedProtocol(v string) bool {
	for _, x := range s.supportedList() {
		if x == v {
			return true
		}
	}
	return false
}

func requestProtocol(r *http.Request, msg map[string]any) string {
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" {
		return v
	}
	if params, ok := msg["params"].(map[string]any); ok {
		if meta, ok := params["_meta"].(map[string]any); ok {
			if v, ok := meta["io.modelcontextprotocol/protocolVersion"].(string); ok {
				return v
			}
		}
		if v, ok := params["protocolVersion"].(string); ok {
			return v
		}
	}
	return ""
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
