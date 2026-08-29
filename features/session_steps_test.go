package features_test

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"
	"github.com/pmdroid/mcp-loadtester/internal/session"
)

func initSession(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the session client headers:$`, w.setSessHeaders)
	sc.Step(`^I open a streamable session$`, w.openSession)
	sc.Step(`^I initialize through the client$`, w.initThroughClient)
	sc.Step(`^I list tools through the client$`, w.listThroughClient)
	sc.Step(`^I call tool "([^"]*)" through the client with query "([^"]*)"$`, w.callThroughClient)
	sc.Step(`^I close the client session$`, w.closeThroughClient)
	sc.Step(`^the client session id is set$`, w.clientSessionSet)
	sc.Step(`^every recorded MCP request has header "([^"]*)" equal to "([^"]*)"$`, w.headerOnEveryRequest)
	sc.Step(`^the last recorded MCP request method is "([^"]*)"$`, w.lastMCPMethod)
	sc.Step(`^the last client error is tagged "([^"]*)"$`, w.lastClientTag)
}

func (w *world) setSessHeaders(table *godog.Table) error {
	w.sessHeaders = map[string]string{}
	for _, row := range table.Rows {
		if len(row.Cells) < 2 {
			return fmt.Errorf("header row needs 2 cells")
		}
		w.sessHeaders[row.Cells[0].Value] = row.Cells[1].Value
	}
	return nil
}

func (w *world) openSession() error {
	if w.mcp == nil {
		return fmt.Errorf("no mcp server")
	}
	w.mcpSess = session.New(session.Config{
		URL:     w.mcp.URL(),
		Headers: w.sessHeaders,
	})
	return nil
}

func (w *world) initThroughClient() error {
	return w.doClient(func(ctx context.Context, c *session.Client) (*session.Result, error) {
		return c.Initialize(ctx)
	})
}

func (w *world) listThroughClient() error {
	return w.doClient(func(ctx context.Context, c *session.Client) (*session.Result, error) {
		return c.Call(ctx, "tools/list", map[string]any{}, time.Time{})
	})
}

func (w *world) callThroughClient(name, query string) error {
	return w.doClient(func(ctx context.Context, c *session.Client) (*session.Result, error) {
		return c.Call(ctx, "tools/call", map[string]any{
			"name":      name,
			"arguments": map[string]any{"query": query},
		}, time.Time{})
	})
}

func (w *world) closeThroughClient() error {
	if w.mcpSess == nil {
		return fmt.Errorf("no session client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w.lastSessErr = w.mcpSess.Close(ctx)
	if w.mcpSess.ID() != "" {
		w.session = w.mcpSess.ID()
	}
	return nil
}

func (w *world) doClient(fn func(context.Context, *session.Client) (*session.Result, error)) error {
	if w.mcpSess == nil {
		return fmt.Errorf("no session client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := fn(ctx, w.mcpSess)
	w.lastSessErr = err
	if res != nil {
		w.lastCT = res.ContentType
		w.lastBody = res.Body
	}
	w.session = w.mcpSess.ID()
	return nil
}

func (w *world) clientSessionSet() error {
	if w.mcpSess == nil || w.mcpSess.ID() == "" {
		return fmt.Errorf("empty session id")
	}
	return nil
}

func (w *world) headerOnEveryRequest(key, val string) error {
	reqs := w.mcp.Requests()
	if len(reqs) == 0 {
		return fmt.Errorf("no mcp requests")
	}
	for i, rec := range reqs {
		if rec.Header.Get(key) != val {
			return fmt.Errorf("request %d %s=%q, want %q", i, key, rec.Header.Get(key), val)
		}
	}
	return nil
}

func (w *world) lastMCPMethod(method string) error {
	reqs := w.mcp.Requests()
	if len(reqs) == 0 {
		return fmt.Errorf("no mcp requests")
	}
	got := reqs[len(reqs)-1].Method
	if got != method {
		return fmt.Errorf("last method %s, want %s", got, method)
	}
	return nil
}

func (w *world) lastClientTag(tag string) error {
	got := session.TagOf(w.lastSessErr)
	if got != tag {
		return fmt.Errorf("tag %q err %v, want %q", got, w.lastSessErr, tag)
	}
	return nil
}
