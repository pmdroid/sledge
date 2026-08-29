package oauth

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pmdroid/sledge/internal/fakes/mcphttp"
	"github.com/pmdroid/sledge/internal/fakes/token"
	"github.com/pmdroid/sledge/internal/secret"
	"github.com/pmdroid/sledge/internal/session"
)

func TestSharedSingleflight(t *testing.T) {
	idp := token.New(token.Options{})
	t.Cleanup(idp.Close)
	mgr, err := New(Config{
		Grant:        GrantClient,
		TokenURL:     idp.URL(),
		ClientID:     idp.ClientID(),
		ClientSecret: secret.New("CLIENT_SECRET", idp.ClientSecret()),
		Scopes:       []string{"mcp.read"},
		TokenScope:   ScopeShared,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := mgr.Token(ctx, "vu")
			errCh <- e
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n := len(idp.Requests()); n != 1 {
		t.Fatalf("token requests %d", n)
	}
}

func TestExpiringRefreshNoAuthError(t *testing.T) {
	idp := token.New(token.Options{ExpiresIn: 1})
	t.Cleanup(idp.Close)
	mgr, err := New(Config{
		Grant:        GrantClient,
		TokenURL:     idp.URL(),
		ClientID:     idp.ClientID(),
		ClientSecret: secret.New("CLIENT_SECRET", idp.ClientSecret()),
		RefreshSkew:  30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := mgr.Token(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Token(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if n := len(idp.Requests()); n != 2 {
		t.Fatalf("token requests %d", n)
	}
}

func TestRefreshRotationMCP(t *testing.T) {
	idp := token.New(token.Options{RotateRefresh: true, ExpiresIn: 1})
	t.Cleanup(idp.Close)
	seed := "rt_seed"
	idp.SeedRefresh(seed)
	mcp := mcphttp.New(mcphttp.Options{Mode: mcphttp.ModeJSON})
	t.Cleanup(mcp.Close)
	mcp.RequireAccess(idp.ValidAccess)
	mgr, err := New(Config{
		Grant:        GrantRefresh,
		TokenURL:     idp.URL(),
		ClientID:     idp.ClientID(),
		ClientSecret: secret.New("CLIENT_SECRET", idp.ClientSecret()),
		RefreshToken: secret.New("REFRESH", seed),
		RefreshSkew:  30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := session.New(session.Config{
		URL: mcp.URL(),
		Auth: func(ctx context.Context, req *http.Request) error {
			return mgr.Inject(ctx, req, "")
		},
	})
	ctx := context.Background()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, "tools/list", map[string]any{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if n := len(idp.Requests()); n != 2 {
		t.Fatalf("token requests %d", n)
	}
}

func TestTokenEndpointFailureTaggedAuth(t *testing.T) {
	idp := token.New(token.Options{})
	t.Cleanup(idp.Close)
	mgr, err := New(Config{
		Grant:        GrantClient,
		TokenURL:     idp.URL(),
		ClientID:     idp.ClientID(),
		ClientSecret: secret.New("CLIENT_SECRET", "wrong"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Token(context.Background(), "")
	if session.TagOf(err) != session.TagAuth {
		t.Fatalf("tag %q err %v", session.TagOf(err), err)
	}
}

func TestPerVUWarnsAndFetchesEach(t *testing.T) {
	idp := token.New(token.Options{})
	t.Cleanup(idp.Close)
	var warn bytes.Buffer
	mgr, err := New(Config{
		Grant:        GrantClient,
		TokenURL:     idp.URL(),
		ClientID:     idp.ClientID(),
		ClientSecret: secret.New("CLIENT_SECRET", idp.ClientSecret()),
		TokenScope:   ScopePerVU,
		Warn:         &warn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn.String(), "per_vu") {
		t.Fatalf("warn %q", warn.String())
	}
	ctx := context.Background()
	if _, err := mgr.Token(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Token(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if n := len(idp.Requests()); n != 2 {
		t.Fatalf("token requests %d", n)
	}
}

func TestTokensAbsentFromLogs(t *testing.T) {
	idp := token.New(token.Options{})
	t.Cleanup(idp.Close)
	mgr, err := New(Config{
		Grant:        GrantClient,
		TokenURL:     idp.URL(),
		ClientID:     idp.ClientID(),
		ClientSecret: secret.New("CLIENT_SECRET", idp.ClientSecret()),
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := mgr.Token(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Log().Debug("access=" + tok + " secret=" + idp.ClientSecret())
	if mgr.Log().Contains(tok) {
		t.Fatal("access leaked")
	}
	if mgr.Log().Contains(idp.ClientSecret()) {
		t.Fatal("secret leaked")
	}
	js, err := mgr.Log().JSONReport()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), tok) {
		t.Fatalf("json leaked %s", js)
	}
}
