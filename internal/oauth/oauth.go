package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pmdroid/sledge/internal/redact"
	"github.com/pmdroid/sledge/internal/secret"
	"github.com/pmdroid/sledge/internal/session"
)

const (
	ScopeShared  = "shared"
	ScopePerVU   = "per_vu"
	GrantClient   = "client_credentials"
	GrantRefresh  = "refresh_token"
	GrantAuthCode = "authorization_code"
	GrantMCP      = "mcp"
)

type Config struct {
	Grant              string
	TokenURL           string
	ClientID           string
	ClientSecret       secret.Secret
	RefreshToken       secret.Secret
	Scopes             []string
	TokenScope         string
	RefreshSkew        time.Duration
	Client             *http.Client
	Now                func() time.Time
	Log                *redact.Logger
	Warn               io.Writer
	InsecureLogSecrets bool
}

type Manager struct {
	grant        string
	tokenURL     string
	clientID     string
	clientSecret secret.Secret
	seedRefresh  secret.Secret
	scopes       []string
	scope        string
	skew         time.Duration
	http         *http.Client
	now          func() time.Time
	log          *redact.Logger
	mu           sync.Mutex
	slots        map[string]*slot
}

type slot struct {
	mu       sync.Mutex
	fetching bool
	waiters  []chan struct{}
	access   string
	refresh  string
	expiry   time.Time
}

type issued struct {
	access  string
	refresh string
	expiry  time.Time
}

func New(cfg Config) (*Manager, error) {
	grant := cfg.Grant
	if grant == GrantMCP {
		grant = GrantAuthCode
	}
	switch grant {
	case GrantClient, GrantRefresh, GrantAuthCode:
	default:
		return nil, fmt.Errorf("unknown oauth grant %q", cfg.Grant)
	}
	scope := cfg.TokenScope
	if scope == "" || scope == "per-vu" {
		if scope == "per-vu" {
			scope = ScopePerVU
		} else {
			scope = ScopeShared
		}
	}
	switch scope {
	case ScopeShared, ScopePerVU:
	default:
		return nil, fmt.Errorf("unknown token_scope %q", cfg.TokenScope)
	}
	skew := cfg.RefreshSkew
	if skew == 0 {
		skew = 30 * time.Second
	}
	cli := cfg.Client
	if cli == nil {
		cli = &http.Client{Timeout: 30 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Log
	if log == nil {
		log = redact.New(cfg.InsecureLogSecrets)
	}
	log.Watch(cfg.ClientSecret.Reveal())
	log.Watch(cfg.RefreshToken.Reveal())
	if cfg.InsecureLogSecrets && cfg.Warn != nil {
		redact.WarnInsecure(cfg.Warn)
	}
	if scope == ScopePerVU && cfg.Warn != nil {
		fmt.Fprintln(cfg.Warn, "WARNING: token_scope per_vu fetches one token per VU and may overload the identity provider")
	}
	return &Manager{
		grant:        grant,
		tokenURL:     cfg.TokenURL,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		seedRefresh:  cfg.RefreshToken,
		scopes:       cfg.Scopes,
		scope:        scope,
		skew:         skew,
		http:         cli,
		now:          now,
		log:          log,
		slots:        map[string]*slot{},
	}, nil
}

func (m *Manager) Log() *redact.Logger {
	return m.log
}

func (m *Manager) Token(ctx context.Context, vu string) (string, error) {
	s := m.slot(vu)
	for {
		s.mu.Lock()
		if s.access != "" && m.now().Before(s.expiry.Add(-m.skew)) {
			tok := s.access
			s.mu.Unlock()
			return tok, nil
		}
		if s.fetching {
			ch := make(chan struct{})
			s.waiters = append(s.waiters, ch)
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", authErr(ctx.Err())
			case <-ch:
				continue
			}
		}
		s.fetching = true
		grant, rt := m.grantPair(s)
		s.mu.Unlock()

		tok, err := m.fetch(ctx, grant, rt)

		s.mu.Lock()
		s.fetching = false
		if err == nil {
			s.access = tok.access
			s.expiry = tok.expiry
			if tok.refresh != "" {
				s.refresh = tok.refresh
			}
			m.log.Watch(tok.access)
			m.log.Watch(tok.refresh)
			m.log.Debug("oauth token fetched")
		}
		waiters := s.waiters
		s.waiters = nil
		s.mu.Unlock()
		for _, w := range waiters {
			close(w)
		}
		if err != nil {
			return "", err
		}
		return tok.access, nil
	}
}

func (m *Manager) Inject(ctx context.Context, req *http.Request, vu string) error {
	tok, err := m.Token(ctx, vu)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

func (m *Manager) slot(vu string) *slot {
	key := "*"
	if m.scope == ScopePerVU {
		key = vu
	}
	m.mu.Lock()
	s := m.slots[key]
	if s == nil {
		s = &slot{}
		m.slots[key] = s
	}
	m.mu.Unlock()
	return s
}

func (m *Manager) grantPair(s *slot) (string, string) {
	if m.grant == GrantRefresh || m.grant == GrantAuthCode {
		rt := s.refresh
		if rt == "" {
			rt = m.seedRefresh.Reveal()
		}
		return GrantRefresh, rt
	}
	return GrantClient, ""
}

func (m *Manager) fetch(ctx context.Context, grant, refresh string) (issued, error) {
	form := url.Values{}
	form.Set("grant_type", grant)
	form.Set("client_id", m.clientID)
	if !m.clientSecret.IsZero() {
		form.Set("client_secret", m.clientSecret.Reveal())
	}
	if grant == GrantRefresh {
		form.Set("refresh_token", refresh)
	}
	if grant == GrantClient && len(m.scopes) > 0 {
		form.Set("scope", strings.Join(m.scopes, " "))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return issued{}, authErr(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.http.Do(req)
	if err != nil {
		return issued{}, authErr(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return issued{}, authErr(err)
	}
	if resp.StatusCode >= 400 {
		return issued{}, authErr(fmt.Errorf("token endpoint http %d", resp.StatusCode))
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return issued{}, authErr(err)
	}
	if raw.AccessToken == "" {
		return issued{}, authErr(fmt.Errorf("token endpoint missing access_token"))
	}
	exp := raw.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}
	return issued{
		access:  raw.AccessToken,
		refresh: raw.RefreshToken,
		expiry:  m.now().Add(time.Duration(exp) * time.Second),
	}, nil
}

func authErr(err error) error {
	if err == nil {
		return nil
	}
	if session.TagOf(err) == session.TagAuth {
		return err
	}
	return &session.Error{Tag: session.TagAuth, Err: err}
}
