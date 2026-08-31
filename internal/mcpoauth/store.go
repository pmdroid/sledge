package mcpoauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Record struct {
	Resource     string    `json:"resource"`
	Issuer       string    `json:"issuer,omitempty"`
	TokenURL     string    `json:"token_url"`
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

func Dir() string {
	if d := os.Getenv("SLEDGE_STATE_DIR"); d != "" {
		return filepath.Join(d, "tokens")
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "sledge", "tokens")
}

func PathFor(resource string) string {
	sum := sha256.Sum256([]byte(resource))
	return filepath.Join(Dir(), hex.EncodeToString(sum[:8])+".json")
}

func Save(rec Record) error {
	if rec.Resource == "" {
		return fmt.Errorf("token record missing resource")
	}
	if rec.RefreshToken == "" {
		return fmt.Errorf("token record missing refresh_token")
	}
	path := PathFor(rec.Resource)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load(resource string) (Record, error) {
	path := PathFor(resource)
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("no stored MCP OAuth token for %s; run: sledge auth <scenario>", resource)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, err
	}
	if rec.RefreshToken == "" {
		return Record{}, fmt.Errorf("stored token for %s is missing refresh_token; run: sledge auth <scenario>", resource)
	}
	return rec, nil
}
