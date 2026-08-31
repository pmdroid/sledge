package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/pmdroid/sledge/internal/mcpoauth"
	"github.com/pmdroid/sledge/internal/redact"
	"github.com/pmdroid/sledge/internal/runner"
	"github.com/pmdroid/sledge/internal/scenario"
	"github.com/pmdroid/sledge/internal/session"
	"github.com/pmdroid/sledge/internal/termstyle"
)

const version = "0.0.0-dev"

var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: sledge <run|validate|auth|version> [scenario]")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "run":
		return cmdRun(args[1:])
	case "validate":
		return cmdValidate(args[1:])
	case "auth":
		return cmdAuth(args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	vus := fs.Int("vus", 0, "")
	dur := fs.String("duration", "", "")
	shared := fs.Bool("http-shared-pool", false, "")
	progress := fs.Bool("progress", false, "")
	color := fs.String("color", "auto", "terminal colors: auto, always, never")
	insecure := fs.Bool("insecure-log-secrets", false, "")
	var outPath string
	fs.StringVar(&outPath, "out", "", "")
	fs.StringVar(&outPath, "out-file", "", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *insecure {
		redact.WarnInsecure(stderr)
	}
	if fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "run: scenario path required")
		return 2
	}
	colorMode, err := parseColorMode(*color)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	sc, err := scenario.LoadFile(fs.Arg(0), scenario.Options{
		VUs:        *vus,
		Duration:   *dur,
		SharedPool: *shared,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	sum, err := runner.Run(context.Background(), runner.Config{
		Scenario:           sc,
		SharedPool:         *shared || sc.HTTP.Pool == "shared",
		InsecureLogSecrets: *insecure,
		Progress:           *progress,
		Color:              colorMode,
		Stdout:             stdout,
		Stderr:             stderr,
		OutPath:            outPath,
	})
	return exitCode(sum, err)
}

func exitCode(sum *runner.Summary, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, err)
		var cerr *runner.ConfigError
		if errors.As(err, &cerr) {
			return 2
		}
		if session.TagOf(err) == session.TagAuth {
			return 2
		}
		return 3
	}
	if sum != nil && sum.Failed {
		return 1
	}
	return 0
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	vus := fs.Int("vus", 0, "")
	dur := fs.String("duration", "", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "validate: scenario path required")
		return 2
	}
	if err := scenario.ValidateFile(fs.Arg(0), scenario.Options{VUs: *vus, Duration: *dur}); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

func cmdAuth(args []string) int {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(stderr)
	callback := fs.String("callback", "127.0.0.1:0", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "auth: scenario path required")
		return 2
	}
	sc, err := scenario.LoadFile(fs.Arg(0), scenario.Options{})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cfg := mcpoauth.LoginConfig{
		Resource:   sc.Target.URL.Reveal(),
		ListenAddr: *callback,
		Stdout:     stdout,
		Stderr:     stderr,
	}
	if sc.Auth != nil && sc.Auth.OAuth != nil {
		cfg.ClientID = sc.Auth.OAuth.ClientID.Reveal()
		cfg.Scopes = sc.Auth.OAuth.Scopes
	}
	if _, err := mcpoauth.Login(context.Background(), cfg); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

func parseColorMode(s string) (termstyle.Mode, error) {
	switch s {
	case "auto", "":
		return termstyle.Auto, nil
	case "always":
		return termstyle.Always, nil
	case "never":
		return termstyle.Never, nil
	default:
		return "", fmt.Errorf("run: invalid --color %q (want auto, always, never)", s)
	}
}
