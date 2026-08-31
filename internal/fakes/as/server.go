package as

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

type Server struct {
	http     *httptest.Server
	resource string
	mu       sync.Mutex
	clientID string
	codes    map[string]string
	refresh  map[string]string
	access   map[string]struct{}
}

func New() *Server {
	s := &Server{
		codes:   map[string]string{},
		refresh: map[string]string{},
		access:  map[string]struct{}{},
	}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	s.resource = s.http.URL + "/mcp"
	return s
}

func (s *Server) Close() {
	s.http.Close()
}

func (s *Server) Resource() string {
	return s.resource
}

func (s *Server) Issuer() string {
	return s.http.URL
}

func (s *Server) TokenURL() string {
	return s.http.URL + "/token"
}

func (s *Server) ProtectedResourceURL() string {
	u, _ := url.Parse(s.resource)
	return s.http.URL + "/.well-known/oauth-protected-resource" + u.EscapedPath()
}

func (s *Server) WWWAuthenticate() string {
	return fmt.Sprintf(`Bearer realm="mcp", resource_metadata="%s"`, s.ProtectedResourceURL())
}

func (s *Server) ValidAccess(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.access[tok]
	return ok
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              s.resource,
			"authorization_servers": []string{s.http.URL},
			"scopes_supported":      []string{"mcp"},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/.well-known/oauth-authorization-server":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                s.http.URL,
			"authorization_endpoint":                s.http.URL + "/authorize",
			"token_endpoint":                        s.http.URL + "/token",
			"registration_endpoint":                 s.http.URL + "/register",
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"none"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/register":
		id := "dcr_" + newID()
		s.mu.Lock()
		s.clientID = id
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": id, "token_endpoint_auth_method": "none"})
	case r.Method == http.MethodGet && r.URL.Path == "/authorize":
		q := r.URL.Query()
		code := "code_" + newID()
		s.mu.Lock()
		s.codes[code] = q.Get("client_id")
		s.mu.Unlock()
		loc := q.Get("redirect_uri")
		u, err := url.Parse(loc)
		if err != nil {
			http.Error(w, "bad redirect", http.StatusBadRequest)
			return
		}
		qq := u.Query()
		qq.Set("code", code)
		qq.Set("state", q.Get("state"))
		u.RawQuery = qq.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	case r.Method == http.MethodPost && r.URL.Path == "/token":
		s.token(w, r)
	case r.URL.Path == "/mcp":
		w.Header().Set("WWW-Authenticate", s.WWWAuthenticate())
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	switch form.Get("grant_type") {
	case "authorization_code":
		code := form.Get("code")
		s.mu.Lock()
		_, ok := s.codes[code]
		delete(s.codes, code)
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		s.issue(w)
	case "refresh_token":
		rt := form.Get("refresh_token")
		s.mu.Lock()
		_, ok := s.refresh[rt]
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		s.issue(w)
	default:
		writeErr(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (s *Server) issue(w http.ResponseWriter) {
	access := "at_" + newID()
	refresh := "rt_" + newID()
	s.mu.Lock()
	s.access[access] = struct{}{}
	s.refresh[refresh] = access
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": refresh,
	})
}

func writeErr(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code})
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
