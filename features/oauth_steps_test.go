package features_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"
	"github.com/pmdroid/sledge/internal/fakes/mcphttp"
	"github.com/pmdroid/sledge/internal/fakes/token"
	"github.com/pmdroid/sledge/internal/oauth"
	"github.com/pmdroid/sledge/internal/redact"
	"github.com/pmdroid/sledge/internal/secret"
	"github.com/pmdroid/sledge/internal/session"
)

func initOAuth(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a fake MCP server using JSON responses requiring bearer$`, w.startMCPBearer)
	sc.Step(`^a fake MCP server using JSON responses requiring bearer token "([^"]*)"$`, w.startMCPStaticBearer)
	sc.Step(`^a fake token endpoint with 1s expiry$`, w.startToken1s)
	sc.Step(`^a seeded refresh token$`, w.seedRefresh)
	sc.Step(`^an oauth manager with grant ([a-z_]+) and token_scope ([a-z_]+)$`, w.startOAuth)
	sc.Step(`^insecure log secrets is enabled$`, w.enableInsecureLogs)
	sc.Step(`^20 VUs fetch a token and initialize MCP$`, w.twentyVUsInit)
	sc.Step(`^a VU initializes MCP twice$`, w.oneVUInitTwice)
	sc.Step(`^a VU initializes MCP then lists tools$`, w.initThenList)
	sc.Step(`^a VU fetches a token$`, w.oneVUToken)
	sc.Step(`^no VU saw an auth error$`, w.noAuthErr)
	sc.Step(`^the known token string is absent from debug logs and the JSON report$`, w.tokenAbsent)
	sc.Step(`^the known token string is present in debug logs$`, w.tokenPresent)
	sc.Step(`^a loud insecure-log-secrets warning was printed$`, w.insecureWarned)
}

func (w *world) startMCPBearer() error {
	if err := w.startMCP(mcphttp.Options{Mode: mcphttp.ModeJSON}); err != nil {
		return err
	}
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	w.mcp.RequireAccess(w.tok.ValidAccess)
	return nil
}

func (w *world) startMCPStaticBearer(tok string) error {
	if err := w.startMCP(mcphttp.Options{Mode: mcphttp.ModeJSON}); err != nil {
		return err
	}
	w.mcp.RequireToken(tok)
	return nil
}

func (w *world) startToken1s() error {
	return w.startToken(token.Options{ExpiresIn: 1})
}

func (w *world) seedRefresh() error {
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	w.seedRT = "rt_seed"
	w.tok.SeedRefresh(w.seedRT)
	return nil
}

func (w *world) enableInsecureLogs() error {
	w.insecureLogs = true
	return nil
}

func (w *world) startOAuth(grant, scope string) error {
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	w.warnBuf = &bytes.Buffer{}
	cfg := oauth.Config{
		Grant:              grant,
		TokenURL:           w.tok.URL(),
		ClientID:           w.tok.ClientID(),
		ClientSecret:       secret.New("CLIENT_SECRET", w.tok.ClientSecret()),
		Scopes:             []string{"mcp.read"},
		TokenScope:         scope,
		RefreshSkew:        30 * time.Second,
		Warn:               w.warnBuf,
		InsecureLogSecrets: w.insecureLogs,
	}
	if grant == oauth.GrantRefresh {
		cfg.RefreshToken = secret.New("REFRESH", w.seedRT)
	}
	mgr, err := oauth.New(cfg)
	if err != nil {
		return err
	}
	w.oauth = mgr
	return nil
}

func (w *world) twentyVUsInit() error {
	return w.runVUs(20, func(ctx context.Context, c *session.Client) error {
		_, err := c.Initialize(ctx)
		return err
	})
}

func (w *world) oneVUInitTwice() error {
	if w.oauth == nil {
		return fmt.Errorf("no oauth")
	}
	if w.mcp == nil {
		return fmt.Errorf("no mcp")
	}
	vu := "vu-0"
	for i := 0; i < 2; i++ {
		c := session.New(session.Config{
			URL: w.mcp.URL(),
			Auth: func(ctx context.Context, req *http.Request) error {
				return w.oauth.Inject(ctx, req, vu)
			},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := c.Initialize(ctx)
		cancel()
		if err != nil {
			w.vuErrs = append(w.vuErrs, err)
		}
	}
	return nil
}

func (w *world) initThenList() error {
	return w.runVUs(1, func(ctx context.Context, c *session.Client) error {
		if _, err := c.Initialize(ctx); err != nil {
			return err
		}
		res, err := c.Call(ctx, "tools/list", map[string]any{}, time.Time{})
		if res != nil {
			w.lastCT = res.ContentType
			w.lastBody = res.Body
		}
		return err
	})
}

func (w *world) oneVUToken() error {
	if w.oauth == nil {
		return fmt.Errorf("no oauth")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.oauth.Token(ctx, "vu")
	if err != nil {
		w.vuErrs = append(w.vuErrs, err)
	}
	return nil
}

func (w *world) runVUs(n int, fn func(context.Context, *session.Client) error) error {
	if w.oauth == nil {
		return fmt.Errorf("no oauth")
	}
	if w.mcp == nil {
		return fmt.Errorf("no mcp")
	}
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			vu := fmt.Sprintf("vu-%d", id)
			c := session.New(session.Config{
				URL: w.mcp.URL(),
				Auth: func(ctx context.Context, req *http.Request) error {
					return w.oauth.Inject(ctx, req, vu)
				},
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errCh <- fn(ctx, c)
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			w.vuErrs = append(w.vuErrs, err)
		}
	}
	return nil
}

func (w *world) noAuthErr() error {
	for _, err := range w.vuErrs {
		if err == nil {
			continue
		}
		if session.TagOf(err) == session.TagAuth {
			return fmt.Errorf("auth error: %v", err)
		}
		return err
	}
	return nil
}

func (w *world) tokenAbsent() error {
	log := w.logger()
	for _, tok := range w.knownSecrets() {
		log.Debug("probe " + tok)
		if log.Contains(tok) {
			return fmt.Errorf("token leaked")
		}
		js, err := log.JSONReport()
		if err != nil {
			return err
		}
		if strings.Contains(string(js), tok) {
			return fmt.Errorf("json leaked token")
		}
	}
	return nil
}

func (w *world) tokenPresent() error {
	log := w.logger()
	for _, tok := range w.tok.AccessTokens() {
		if log.Contains(tok) {
			return nil
		}
		log.Debug("access=" + tok)
		if log.Contains(tok) {
			return nil
		}
	}
	return fmt.Errorf("token not in logs")
}

func (w *world) insecureWarned() error {
	if w.warnBuf == nil || !strings.Contains(w.warnBuf.String(), "insecure-log-secrets") || !strings.Contains(w.warnBuf.String(), "WARNING") {
		var s string
		if w.warnBuf != nil {
			s = w.warnBuf.String()
		}
		return fmt.Errorf("missing warning: %q", s)
	}
	return nil
}

func (w *world) logger() *redact.Logger {
	if w.oauth != nil {
		return w.oauth.Log()
	}
	return redact.New(w.insecureLogs)
}

func (w *world) knownSecrets() []string {
	var out []string
	if w.tok != nil {
		out = append(out, w.tok.AccessTokens()...)
		out = append(out, w.tok.ClientSecret())
	}
	if w.seedRT != "" {
		out = append(out, w.seedRT)
	}
	return out
}
