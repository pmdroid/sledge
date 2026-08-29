package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
	"github.com/pmdroid/sledge/internal/scenario"
)

type valWorld struct {
	dir  string
	path string
	err  error
	env  map[string]string
}

func initValidate(sc *godog.ScenarioContext) {
	w := &valWorld{env: map[string]string{}}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		dir, err := os.MkdirTemp("", "sledge-validate-")
		if err != nil {
			return ctx, err
		}
		w.dir = dir
		w.path = ""
		w.err = nil
		w.env = map[string]string{}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if w.dir != "" {
			os.RemoveAll(w.dir)
		}
		return ctx, nil
	})
	sc.Step(`^env ([A-Z0-9_]+) is "([^"]*)"$`, func(key, val string) error {
		w.env[key] = val
		return nil
	})
	sc.Step(`^a scenario file:$`, func(body *godog.DocString) error {
		w.path = filepath.Join(w.dir, "scenario.yaml")
		return os.WriteFile(w.path, []byte(body.Content), 0o600)
	})
	sc.Step(`^I validate the scenario$`, func() error {
		opts := scenario.Options{
			LookupEnv: func(k string) (string, bool) {
				if v, ok := w.env[k]; ok {
					return v, true
				}
				return os.LookupEnv(k)
			},
		}
		w.err = scenario.ValidateFile(w.path, opts)
		return nil
	})
	sc.Step(`^validation succeeds$`, func() error {
		if w.err != nil {
			return w.err
		}
		return nil
	})
	sc.Step(`^validation fails with "([^"]*)"$`, func(want string) error {
		if w.err == nil {
			return fmt.Errorf("expected failure containing %q", want)
		}
		if !strings.Contains(w.err.Error(), want) {
			return fmt.Errorf("error %q, want %q", w.err, want)
		}
		return nil
	})
}
