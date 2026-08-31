package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type ResourceMetadata struct {
	Resource              string   `json:"resource"`
	AuthorizationServers  []string `json:"authorization_servers"`
	ScopesSupported       []string `json:"scopes_supported"`
}

type ServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
}

func ParseWWWAuthenticate(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	const key = "resource_metadata="
	i := strings.Index(strings.ToLower(h), key)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(h[i+len(key):])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return ""
		}
		return rest[1 : 1+end]
	}
	end := len(rest)
	for j, c := range rest {
		if c == ',' || c == ' ' || c == ';' {
			end = j
			break
		}
	}
	return rest[:end]
}

func ProtectedResourceURL(resource string) (string, error) {
	u, err := url.Parse(resource)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid resource URL %q", resource)
	}
	path := u.EscapedPath()
	if path == "/" {
		path = ""
	}
	u.Path = "/.well-known/oauth-protected-resource" + path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func AuthorizationServerURL(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid issuer URL %q", issuer)
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if path == "/" {
		path = ""
	}
	u.Path = "/.well-known/oauth-authorization-server" + path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func FetchResourceMetadata(ctx context.Context, client *http.Client, metaURL string) (*ResourceMetadata, error) {
	var out ResourceMetadata
	if err := getJSON(ctx, client, metaURL, &out); err != nil {
		return nil, err
	}
	if len(out.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("protected resource metadata missing authorization_servers")
	}
	return &out, nil
}

func FetchServerMetadata(ctx context.Context, client *http.Client, issuer string) (*ServerMetadata, error) {
	metaURL, err := AuthorizationServerURL(issuer)
	if err != nil {
		return nil, err
	}
	var out ServerMetadata
	if err := getJSON(ctx, client, metaURL, &out); err != nil {
		return nil, err
	}
	if out.AuthorizationEndpoint == "" || out.TokenEndpoint == "" {
		return nil, fmt.Errorf("authorization server metadata missing endpoints")
	}
	if out.Issuer == "" {
		out.Issuer = issuer
	}
	return &out, nil
}

func getJSON(ctx context.Context, client *http.Client, rawURL string, dest any) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %s: http %d", rawURL, resp.StatusCode)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return err
	}
	return nil
}
