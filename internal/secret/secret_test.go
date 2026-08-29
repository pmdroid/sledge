package secret

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRedacted(t *testing.T) {
	s := New("TOKEN", "super-secret")
	if s.String() != "[redacted]" {
		t.Fatalf("String %q", s.String())
	}
	if s.GoString() != "secret.Secret{[redacted]}" {
		t.Fatalf("GoString %q", s.GoString())
	}
	if fmt.Sprint(s) == "super-secret" {
		t.Fatal("Sprint leaked")
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"[redacted]"` {
		t.Fatalf("json %s", b)
	}
	y, err := yaml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(y), "super-secret") || !strings.Contains(string(y), "redacted") {
		t.Fatalf("yaml %q", y)
	}
	if s.Reveal() != "super-secret" {
		t.Fatalf("Reveal %q", s.Reveal())
	}
	if s.Name() != "TOKEN" {
		t.Fatalf("Name %q", s.Name())
	}
}
