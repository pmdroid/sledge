package metrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Name   string `json:"name"`
	Expr   string `json:"expr"`
	Actual string `json:"actual"`
	OK     bool   `json:"ok"`
}

var exprRE = regexp.MustCompile(`^(<=|>=|==|!=|<|>)\s*(.+)$`)

func Evaluate(thresholds map[string]string, snap Snapshot) ([]Result, error) {
	if len(thresholds) == 0 {
		return nil, nil
	}
	out := make([]Result, 0, len(thresholds))
	keys := []string{"error_rate", "p95_latency", "auth_errors"}
	seen := map[string]bool{}
	for _, k := range keys {
		expr, ok := thresholds[k]
		if !ok {
			continue
		}
		seen[k] = true
		r, err := evalOne(k, expr, snap)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	for k := range thresholds {
		if !seen[k] {
			return nil, fmt.Errorf("unknown threshold %q", k)
		}
	}
	return out, nil
}

func Failed(rs []Result) bool {
	for _, r := range rs {
		if !r.OK {
			return true
		}
	}
	return false
}

func evalOne(name, expr string, snap Snapshot) (Result, error) {
	op, raw, err := splitExpr(expr)
	if err != nil {
		return Result{}, fmt.Errorf("threshold %s: %w", name, err)
	}
	r := Result{Name: name, Expr: expr}
	switch name {
	case "error_rate":
		want, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return Result{}, fmt.Errorf("threshold %s: invalid number %q", name, raw)
		}
		r.Actual = strconv.FormatFloat(snap.ErrorRate, 'f', 6, 64)
		r.OK = cmpFloat(op, snap.ErrorRate, want)
	case "auth_errors":
		want, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return Result{}, fmt.Errorf("threshold %s: invalid number %q", name, raw)
		}
		got := float64(snap.Failures.Auth)
		r.Actual = strconv.FormatInt(snap.Failures.Auth, 10)
		r.OK = cmpFloat(op, got, want)
	case "p95_latency":
		want, err := time.ParseDuration(raw)
		if err != nil {
			return Result{}, fmt.Errorf("threshold %s: invalid duration %q", name, raw)
		}
		r.Actual = snap.P95.String()
		r.OK = cmpDur(op, snap.P95, want)
	default:
		return Result{}, fmt.Errorf("unknown threshold %q", name)
	}
	return r, nil
}

func splitExpr(expr string) (string, string, error) {
	expr = strings.TrimSpace(expr)
	m := exprRE.FindStringSubmatch(expr)
	if m == nil {
		return "", "", fmt.Errorf("invalid expression %q", expr)
	}
	return m[1], strings.TrimSpace(m[2]), nil
}

func cmpFloat(op string, got, want float64) bool {
	switch op {
	case "<":
		return got < want
	case "<=":
		return got <= want
	case ">":
		return got > want
	case ">=":
		return got >= want
	case "==":
		return got == want
	case "!=":
		return got != want
	default:
		return false
	}
}

func cmpDur(op string, got, want time.Duration) bool {
	switch op {
	case "<":
		return got < want
	case "<=":
		return got <= want
	case ">":
		return got > want
	case ">=":
		return got >= want
	case "==":
		return got == want
	case "!=":
		return got != want
	default:
		return false
	}
}
