package termstyle

import (
	"fmt"
	"io"
	"os"
)

type Mode string

const (
	Auto   Mode = "auto"
	Always Mode = "always"
	Never  Mode = "never"
)

type Theme struct {
	on bool

	reset  string
	bold   string
	dim    string
	orange string
	green  string
	cyan   string
	yellow string
	red    string
}

func New(w io.Writer, mode Mode) *Theme {
	if !enabled(w, mode) {
		return &Theme{}
	}
	return &Theme{
		on:     true,
		reset:  "\033[0m",
		bold:   "\033[1m",
		dim:    "\033[2m",
		orange: "\033[38;5;208m",
		green:  "\033[38;5;82m",
		cyan:   "\033[38;5;51m",
		yellow: "\033[38;5;220m",
		red:    "\033[38;5;196m",
	}
}

func (t *Theme) On() bool {
	return t != nil && t.on
}

func (t *Theme) Reset() string {
	if t == nil || !t.on {
		return ""
	}
	return t.reset
}

func (t *Theme) Label(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.green + s + t.reset
}

func (t *Theme) Value(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.bold + s + t.reset
}

func (t *Theme) Accent(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.cyan + s + t.reset
}

func (t *Theme) Dim(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.dim + s + t.reset
}

func (t *Theme) Good(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.green + s + t.reset
}

func (t *Theme) Bad(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.red + s + t.reset
}

func (t *Theme) Warn(s string) string {
	if t == nil || !t.on {
		return s
	}
	return t.yellow + s + t.reset
}

func (t *Theme) Sledge() string {
	if t == nil || !t.on {
		return "SLEDGE"
	}
	return t.bold + t.orange + "SLEDGE" + t.reset
}

func (t *Theme) BannerLine(left, right string) string {
	if t == nil || !t.on {
		return fmt.Sprintf("  %s  %s", left, right)
	}
	return fmt.Sprintf("  %s  %s", t.Sledge(), right)
}

func enabled(w io.Writer, mode Mode) bool {
	switch mode {
	case Never:
		return false
	case Always:
		return true
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") != "" {
		return true
	}
	return isCharDevice(w)
}

func isCharDevice(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
