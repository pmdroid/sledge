package token

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
)

const (
	DefaultClientID     = "client"
	DefaultClientSecret = "secret"
)

type Options struct {
	ClientID      string
	ClientSecret  string
	ExpiresIn     int
	RotateRefresh bool
	Public        bool
}

type Recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
	Form   url.Values
}

type Server struct {
	http         *httptest.Server
	clientID     string
	clientSecret string
	expiresIn    int
	rotate       bool
	public       bool
	mu           sync.Mutex
	reqs         []Recorded
	refresh      map[string]string
	access       map[string]struct{}
}

func New(opts Options) *Server {
	s := &Server{
		clientID:     opts.ClientID,
		clientSecret: opts.ClientSecret,
		expiresIn:    opts.ExpiresIn,
		rotate:       opts.RotateRefresh,
		public:       opts.Public,
		refresh:      map[string]string{},
		access:       map[string]struct{}{},
	}
	if s.clientID == "" {
		s.clientID = DefaultClientID
	}
	if s.clientSecret == "" && !s.public {
		s.clientSecret = DefaultClientSecret
	}
	if s.expiresIn == 0 {
		s.expiresIn = 3600
	}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *Server) URL() string {
	return s.http.URL + "/token"
}

func (s *Server) Close() {
	s.http.Close()
}

func (s *Server) ClientID() string {
	return s.clientID
}

func (s *Server) ClientSecret() string {
	return s.clientSecret
}

func (s *Server) SeedRefresh(rt string) {
	s.mu.Lock()
	s.refresh[rt] = ""
	s.mu.Unlock()
}

func (s *Server) ValidAccess(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.access[tok]
	return ok
}

func (s *Server) AccessTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.access))
	for t := range s.access {
		out = append(out, t)
	}
	return out
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
		oauthError(w, http.StatusBadRequest, "invalid_request", "read body")
		return
	}
	form, _ := url.ParseQuery(string(body))
	s.mu.Lock()
	s.reqs = append(s.reqs, Recorded{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Body:   body,
		Form:   form,
	})
	s.mu.Unlock()

	if r.Method != http.MethodPost || r.URL.Path != "/token" {
		http.NotFound(w, r)
		return
	}

	cid, csec := form.Get("client_id"), form.Get("client_secret")
	if u, p, ok := r.BasicAuth(); ok {
		cid, csec = u, p
	}
	if cid != s.clientID || (!s.public && csec != s.clientSecret) {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "bad client")
		return
	}

	switch form.Get("grant_type") {
	case "client_credentials":
		s.issue(w, "")
	case "refresh_token":
		old := form.Get("refresh_token")
		s.mu.Lock()
		_, ok := s.refresh[old]
		s.mu.Unlock()
		if !ok {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "unknown refresh")
			return
		}
		s.issue(w, old)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported")
	}
}

func (s *Server) issue(w http.ResponseWriter, oldRefresh string) {
	access := "at_" + newID()
	refresh := "rt_" + newID()
	s.mu.Lock()
	if oldRefresh != "" {
		if prev := s.refresh[oldRefresh]; prev != "" {
			delete(s.access, prev)
		}
		delete(s.refresh, oldRefresh)
		if !s.rotate {
			refresh = oldRefresh
		}
	}
	s.access[access] = struct{}{}
	s.refresh[refresh] = access
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    s.expiresIn,
		"refresh_token": refresh,
	})
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":             code,
		"error_description": desc,
	})
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
