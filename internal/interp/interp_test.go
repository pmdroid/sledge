package interp

import (
	"os"
	"testing"
)

func TestParseAndResolve(t *testing.T) {
	t.Setenv("FOO", "bar")
	v, err := Parse("$$ and ${env:FOO} ${var:q} ${secret:T} ${vu.id} ${iteration}")
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasSecret() {
		t.Fatal("expected secret")
	}
	got, err := v.Resolve(Context{
		Env:  os.LookupEnv,
		Vars: map[string]string{"q": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "$ and bar hello [redacted] ${vu.id} ${iteration}" {
		t.Fatalf("got %q", got.String())
	}
	if got.Reveal() != "$ and bar hello  ${vu.id} ${iteration}" {
		t.Fatalf("reveal %q", got.Reveal())
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{"$foo", "${", "${nope}", "${env:}", "${foo:bar}"}
	for _, c := range cases {
		if _, err := Parse(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestMissingEnv(t *testing.T) {
	v, err := Parse("${env:NO_SUCH_MCLOAD_VAR}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Resolve(Context{Env: os.LookupEnv}); err == nil {
		t.Fatal("expected missing env")
	}
}
