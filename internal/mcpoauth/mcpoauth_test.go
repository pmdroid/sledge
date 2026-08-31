package mcpoauth

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/pmdroid/sledge/internal/fakes/as"
)

func TestParseWWWAuthenticate(t *testing.T) {
	got := ParseWWWAuthenticate(`Bearer realm="mcp", resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp"`)
	if got != "https://example.com/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("got %q", got)
	}
	got = ParseWWWAuthenticate(`Bearer resource_metadata=https://example.com/prm`)
	if got != "https://example.com/prm" {
		t.Fatalf("got %q", got)
	}
	if ParseWWWAuthenticate("") != "" {
		t.Fatal("empty")
	}
}

func TestProtectedResourceURL(t *testing.T) {
	got, err := ProtectedResourceURL("https://api.example.com/mcp/gw_1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/.well-known/oauth-protected-resource/mcp/gw_1" {
		t.Fatalf("got %q", got)
	}
	got, err = ProtectedResourceURL("https://api.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("got %q", got)
	}
}

func TestAuthorizationServerURL(t *testing.T) {
	got, err := AuthorizationServerURL("https://cloud.example.com/oauth2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cloud.example.com/.well-known/oauth-authorization-server/oauth2" {
		t.Fatalf("got %q", got)
	}
}

func TestPKCE(t *testing.T) {
	p, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if p.Verifier == "" || p.Challenge == "" || p.Verifier == p.Challenge {
		t.Fatalf("%+v", p)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SLEDGE_STATE_DIR", dir)
	rec := Record{
		Resource:     "https://example.com/mcp",
		TokenURL:     "https://idp/token",
		ClientID:     "cid",
		RefreshToken: "rt-secret",
		AccessToken:  "at-secret",
	}
	if err := Save(rec); err != nil {
		t.Fatal(err)
	}
	path := PathFor(rec.Resource)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
	got, err := Load(rec.Resource)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "rt-secret" || got.ClientID != "cid" {
		t.Fatalf("%+v", got)
	}
}

func TestLoadMissing(t *testing.T) {
	t.Setenv("SLEDGE_STATE_DIR", t.TempDir())
	_, err := Load("https://example.com/mcp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLogin(t *testing.T) {
	idp := as.New()
	t.Cleanup(idp.Close)
	t.Setenv("SLEDGE_STATE_DIR", t.TempDir())
	ctx := context.Background()
	rec, err := Login(ctx, LoginConfig{
		Resource: idp.Resource(),
		Stderr:   io.Discard,
		Stdout:   io.Discard,
		OpenURL: func(u string) error {
			resp, err := http.Get(u)
			if err != nil {
				return err
			}
			resp.Body.Close()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.RefreshToken == "" || rec.ClientID == "" || rec.TokenURL == "" {
		t.Fatalf("%+v", rec)
	}
	st, err := os.Stat(PathFor(idp.Resource()))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
	if rec.RefreshToken != "" && filepath.Ext(PathFor(idp.Resource())) != ".json" {
		t.Fatal("path")
	}
}
