package termstyle

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNeverPlain(t *testing.T) {
	sty := New(&bytes.Buffer{}, Never)
	if sty.On() {
		t.Fatal("expected plain")
	}
	if got := sty.Label("vus:"); got != "vus:" {
		t.Fatalf("got %q", got)
	}
}

func TestAlwaysAddsCodes(t *testing.T) {
	sty := New(&bytes.Buffer{}, Always)
	if !sty.On() {
		t.Fatal("expected color")
	}
	got := sty.Label("vus:")
	if !strings.Contains(got, "\033[") {
		t.Fatalf("expected ansi: %q", got)
	}
	if !strings.Contains(got, "vus:") {
		t.Fatalf("expected literal: %q", got)
	}
}

func TestAutoRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	sty := New(os.Stdout, Auto)
	if sty.On() {
		t.Fatal("NO_COLOR should disable")
	}
}
