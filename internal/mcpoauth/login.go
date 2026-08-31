package mcpoauth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/pmdroid/sledge/internal/session"
)

type LoginConfig struct {
	Resource   string
	ClientID   string
	Scopes     []string
	ListenAddr string
	HTTP       *http.Client
	OpenURL    func(string) error
	Stdout     io.Writer
	Stderr     io.Writer
}

func Login(ctx context.Context, cfg LoginConfig) (Record, error) {
	if cfg.Resource == "" {
		return Record{}, fmt.Errorf("missing resource URL")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	as, prm, err := discover(ctx, cfg)
	if err != nil {
		return Record{}, err
	}
	if as.RegistrationEndpoint == "" && cfg.ClientID == "" {
		return Record{}, fmt.Errorf("authorization server has no registration_endpoint and no client_id is set")
	}

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return Record{}, err
	}
	defer ln.Close()
	redirectURI := "http://" + ln.Addr().String() + "/callback"

	clientID := cfg.ClientID
	if clientID == "" {
		reg, err := RegisterClient(ctx, cfg.HTTP, as.RegistrationEndpoint, redirectURI)
		if err != nil {
			return Record{}, err
		}
		clientID = reg.ClientID
	}

	pkce, err := NewPKCE()
	if err != nil {
		return Record{}, err
	}
	state, err := NewState()
	if err != nil {
		return Record{}, err
	}
	resource := prm.Resource
	if resource == "" {
		resource = cfg.Resource
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = prm.ScopesSupported
	}
	authURL := AuthorizeURL(as.AuthorizationEndpoint, clientID, redirectURI, pkce.Challenge, state, resource, scopes)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("error") != "" {
			msg := r.URL.Query().Get("error_description")
			if msg == "" {
				msg = r.URL.Query().Get("error")
			}
			http.Error(w, msg, http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization failed: %s", msg)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization response missing code")
			return
		}
		_, _ = io.WriteString(w, "sledge is authorized. You can close this tab.")
		codeCh <- code
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = srv.Shutdown(cctx)
		cancel()
	}()

	fmt.Fprintln(stderr, "Open this URL to authorize sledge:")
	fmt.Fprintln(stdout, authURL)
	if cfg.OpenURL != nil {
		if err := cfg.OpenURL(authURL); err != nil {
			return Record{}, err
		}
	}

	var code string
	select {
	case <-ctx.Done():
		return Record{}, ctx.Err()
	case err := <-errCh:
		return Record{}, err
	case code = <-codeCh:
	}

	tok, err := Exchange(ctx, cfg.HTTP, as.TokenEndpoint, clientID, code, redirectURI, pkce.Verifier, resource)
	if err != nil {
		return Record{}, err
	}
	if tok.RefreshToken == "" {
		return Record{}, fmt.Errorf("token endpoint did not return a refresh_token")
	}
	rec := Record{
		Resource:     cfg.Resource,
		Issuer:       as.Issuer,
		TokenURL:     as.TokenEndpoint,
		ClientID:     clientID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}
	if err := Save(rec); err != nil {
		return Record{}, err
	}
	fmt.Fprintf(stderr, "saved refresh token to %s\n", PathFor(rec.Resource))
	return rec, nil
}

func discover(ctx context.Context, cfg LoginConfig) (*ServerMetadata, *ResourceMetadata, error) {
	metaURL := ""
	cli := session.New(session.Config{URL: cfg.Resource, Client: cfg.HTTP})
	res, err := cli.Initialize(ctx)
	if res != nil {
		metaURL = ParseWWWAuthenticate(res.WWWAuthenticate)
	}
	if metaURL == "" {
		built, berr := ProtectedResourceURL(cfg.Resource)
		if berr != nil {
			if err != nil {
				return nil, nil, err
			}
			return nil, nil, berr
		}
		metaURL = built
	}
	prm, err := FetchResourceMetadata(ctx, cfg.HTTP, metaURL)
	if err != nil {
		return nil, nil, err
	}
	as, err := FetchServerMetadata(ctx, cfg.HTTP, prm.AuthorizationServers[0])
	if err != nil {
		return nil, nil, err
	}
	return as, prm, nil
}
