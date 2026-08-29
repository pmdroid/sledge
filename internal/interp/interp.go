package interp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/pmdroid/mcp-loadtester/internal/secret"
)

type kind int

const (
	kindLit kind = iota
	kindEnv
	kindSecret
	kindVar
	kindVUID
	kindIter
)

type part struct {
	kind kind
	lit  string
	name string
	sec  secret.Secret
}

type Value struct {
	parts []part
}

type Context struct {
	Env       func(string) (string, bool)
	Vars      map[string]string
	Secrets   func(string) (string, bool)
	VU        string
	Iteration string
}

func Parse(s string) (Value, error) {
	var parts []part
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			j := i + 1
			for j < len(s) && s[j] != '$' {
				j++
			}
			parts = append(parts, part{kind: kindLit, lit: s[i:j]})
			i = j
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			parts = append(parts, part{kind: kindLit, lit: "$"})
			i += 2
			continue
		}
		if i+1 >= len(s) || s[i+1] != '{' {
			return Value{}, fmt.Errorf("unescaped $")
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return Value{}, fmt.Errorf("unclosed interpolation")
		}
		inner := s[i+2 : i+end]
		p, err := parseInner(inner)
		if err != nil {
			return Value{}, err
		}
		parts = append(parts, p)
		i += end + 1
	}
	return Value{parts: parts}, nil
}

func parseInner(inner string) (part, error) {
	switch inner {
	case "vu.id":
		return part{kind: kindVUID}, nil
	case "iteration":
		return part{kind: kindIter}, nil
	}
	key, name, ok := strings.Cut(inner, ":")
	if !ok || name == "" {
		return part{}, fmt.Errorf("unknown interpolation ${%s}", inner)
	}
	if !ident(name) {
		return part{}, fmt.Errorf("invalid interpolation name %q", name)
	}
	switch key {
	case "env":
		return part{kind: kindEnv, name: name}, nil
	case "secret":
		return part{kind: kindSecret, name: name, sec: secret.New(name, "")}, nil
	case "var":
		return part{kind: kindVar, name: name}, nil
	default:
		return part{}, fmt.Errorf("unknown interpolation ${%s}", inner)
	}
}

func ident(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && r != '_' && !unicode.IsLetter(r) {
			return false
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func (v Value) HasSecret() bool {
	for _, p := range v.parts {
		if p.kind == kindSecret {
			return true
		}
	}
	return false
}

func (v Value) Resolve(ctx Context) (Value, error) {
	out := make([]part, 0, len(v.parts))
	for _, p := range v.parts {
		switch p.kind {
		case kindEnv:
			if ctx.Env == nil {
				return Value{}, fmt.Errorf("environment variable %s is not set", p.name)
			}
			val, ok := ctx.Env(p.name)
			if !ok {
				return Value{}, fmt.Errorf("environment variable %s is not set", p.name)
			}
			out = append(out, part{kind: kindLit, lit: val})
		case kindVar:
			if ctx.Vars == nil {
				return Value{}, fmt.Errorf("variable %s is not set", p.name)
			}
			val, ok := ctx.Vars[p.name]
			if !ok {
				return Value{}, fmt.Errorf("variable %s is not set", p.name)
			}
			out = append(out, part{kind: kindLit, lit: val})
		case kindSecret:
			if ctx.Secrets != nil {
				if val, ok := ctx.Secrets(p.name); ok {
					out = append(out, part{kind: kindSecret, name: p.name, sec: secret.New(p.name, val)})
					continue
				}
			}
			out = append(out, p)
		case kindVUID:
			if ctx.VU != "" {
				out = append(out, part{kind: kindLit, lit: ctx.VU})
				continue
			}
			out = append(out, p)
		case kindIter:
			if ctx.Iteration != "" {
				out = append(out, part{kind: kindLit, lit: ctx.Iteration})
				continue
			}
			out = append(out, p)
		default:
			out = append(out, p)
		}
	}
	return Value{parts: out}, nil
}

func (v Value) String() string {
	var b strings.Builder
	for _, p := range v.parts {
		switch p.kind {
		case kindLit:
			b.WriteString(p.lit)
		case kindSecret:
			b.WriteString(p.sec.String())
		case kindVUID:
			b.WriteString("${vu.id}")
		case kindIter:
			b.WriteString("${iteration}")
		case kindEnv:
			b.WriteString("${env:" + p.name + "}")
		case kindVar:
			b.WriteString("${var:" + p.name + "}")
		}
	}
	return b.String()
}

func (v Value) Reveal() string {
	var b strings.Builder
	for _, p := range v.parts {
		switch p.kind {
		case kindLit:
			b.WriteString(p.lit)
		case kindSecret:
			b.WriteString(p.sec.Reveal())
		case kindVUID:
			b.WriteString("${vu.id}")
		case kindIter:
			b.WriteString("${iteration}")
		case kindEnv:
			b.WriteString("${env:" + p.name + "}")
		case kindVar:
			b.WriteString("${var:" + p.name + "}")
		}
	}
	return b.String()
}

func (v Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

func (v Value) MarshalYAML() (any, error) {
	return v.String(), nil
}

func (v Value) SecretNames() []string {
	var names []string
	for _, p := range v.parts {
		if p.kind == kindSecret {
			names = append(names, p.name)
		}
	}
	return names
}
