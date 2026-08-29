package redact

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

const InsecureWarning = "WARNING: --insecure-log-secrets is enabled; secrets and access tokens will be written to logs and JSON reports"

func WarnInsecure(w io.Writer) {
	fmt.Fprintln(w, InsecureWarning)
}

type Logger struct {
	insecure bool
	mu       sync.Mutex
	secrets  []string
	debug    []string
}

func New(insecure bool) *Logger {
	return &Logger{insecure: insecure}
}

func (l *Logger) Watch(v string) {
	if l == nil || v == "" {
		return
	}
	l.mu.Lock()
	l.secrets = append(l.secrets, v)
	l.mu.Unlock()
}

func (l *Logger) Debug(msg string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.debug = append(l.debug, l.scrubLocked(msg))
	l.mu.Unlock()
}

func (l *Logger) Logs() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.debug, "\n")
}

func (l *Logger) JSONReport() ([]byte, error) {
	if l == nil {
		return json.Marshal(map[string]any{"debug": []string{}})
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	debug := make([]string, len(l.debug))
	copy(debug, l.debug)
	return json.Marshal(map[string]any{"debug": debug})
}

func (l *Logger) Insecure() bool {
	return l != nil && l.insecure
}

func (l *Logger) Scrub(s string) string {
	if l == nil {
		return s
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scrubLocked(s)
}

func (l *Logger) Contains(s string) bool {
	if l == nil || s == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.Contains(strings.Join(l.debug, "\n"), s) {
		return true
	}
	raw, err := json.Marshal(map[string]any{"debug": l.debug})
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), s)
}

func (l *Logger) scrubLocked(msg string) string {
	if l.insecure {
		return msg
	}
	secs := make([]string, len(l.secrets))
	copy(secs, l.secrets)
	sort.Slice(secs, func(i, j int) bool { return len(secs[i]) > len(secs[j]) })
	for _, s := range secs {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "[redacted]")
		}
	}
	return msg
}
