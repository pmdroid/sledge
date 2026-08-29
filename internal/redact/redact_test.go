package redact

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactsUnlessInsecure(t *testing.T) {
	tok := "at_super_secret_token"
	l := New(false)
	l.Watch(tok)
	l.Debug("got " + tok)
	if l.Contains(tok) {
		t.Fatal("token leaked")
	}
	js, err := l.JSONReport()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), tok) {
		t.Fatalf("json leaked %s", js)
	}
	if !strings.Contains(l.Logs(), "[redacted]") {
		t.Fatalf("logs %q", l.Logs())
	}
}

func TestInsecureKeepsToken(t *testing.T) {
	tok := "at_super_secret_token"
	l := New(true)
	l.Watch(tok)
	l.Debug("got " + tok)
	if !l.Contains(tok) {
		t.Fatal("expected token when insecure")
	}
}

func TestScrub(t *testing.T) {
	l := New(false)
	l.Watch("sekrit")
	got := l.Scrub("Bearer sekrit")
	if strings.Contains(got, "sekrit") {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("%q", got)
	}
}

func TestWarnInsecure(t *testing.T) {
	var b bytes.Buffer
	WarnInsecure(&b)
	if !strings.Contains(b.String(), "WARNING") || !strings.Contains(b.String(), "insecure-log-secrets") {
		t.Fatalf("warn %q", b.String())
	}
}
