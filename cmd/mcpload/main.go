package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/pmdroid/mcp-loadtester/internal/redact"
	"github.com/pmdroid/mcp-loadtester/internal/runner"
	"github.com/pmdroid/mcp-loadtester/internal/scenario"
	"github.com/pmdroid/mcp-loadtester/internal/session"
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
		fmt.Fprintln(stderr, "usage: mcpload <run|validate|version> [scenario]")
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
	insecure := fs.Bool("insecure-log-secrets", false, "")
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
	sc, err := scenario.LoadFile(fs.Arg(0), scenario.Options{
		VUs:        *vus,
		Duration:   *dur,
		SharedPool: *shared,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	_, err = runner.Run(context.Background(), runner.Config{
		Scenario:           sc,
		SharedPool:         *shared || sc.HTTP.Pool == "shared",
		InsecureLogSecrets: *insecure,
		Stdout:             stdout,
		Stderr:             stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		if session.TagOf(err) == session.TagAuth {
			return 2
		}
		return 3
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
