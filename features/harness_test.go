package features_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/pmdroid/mcp-loadtester/internal/fakes/mcphttp"
	"github.com/pmdroid/mcp-loadtester/internal/fakes/token"
	"github.com/pmdroid/mcp-loadtester/internal/session"
)

type world struct {
	mcp         *mcphttp.Server
	tok         *token.Server
	client      *http.Client
	access      string
	refresh     string
	oldRefresh  string
	session     string
	expiresIn   int
	lastStatus  int
	lastCT      string
	lastBody    []byte
	lastHeader  http.Header
	lastErr     error
	sessHeaders map[string]string
	mcpSess     *session.Client
	lastSessErr error
}

func (w *world) close() {
	if w.mcp != nil {
		w.mcp.Close()
		w.mcp = nil
	}
	if w.tok != nil {
		w.tok.Close()
		w.tok = nil
	}
}

func (w *world) reset() {
	w.close()
	*w = world{client: &http.Client{Timeout: 2 * time.Second}}
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "harness",
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"."},
			TestingT: t,
			Strict:   true,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("godog failed")
	}
}

func InitializeScenario(sc *godog.ScenarioContext) {
	initValidate(sc)
	w := &world{}
	initSession(sc, w)
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.reset()
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.close()
		return ctx, nil
	})
	sc.Step(`^a fake token endpoint$`, func() error {
		return w.startToken(token.Options{})
	})
	sc.Step(`^a fake token endpoint with refresh rotation and 1s expiry$`, func() error {
		return w.startToken(token.Options{RotateRefresh: true, ExpiresIn: 1})
	})
	sc.Step(`^a fake MCP server using JSON responses$`, func() error {
		return w.startMCP(mcphttp.Options{Mode: mcphttp.ModeJSON})
	})
	sc.Step(`^a fake MCP server using SSE responses$`, func() error {
		return w.startMCP(mcphttp.Options{Mode: mcphttp.ModeSSE})
	})
	sc.Step(`^a fake MCP server using slow SSE responses$`, func() error {
		return w.startMCP(mcphttp.Options{Mode: mcphttp.ModeSSESlow, SlowDelay: 20 * time.Millisecond})
	})
	sc.Step(`^a fake MCP server that disconnects mid-stream$`, func() error {
		w.session = ""
		return w.startMCP(mcphttp.Options{Mode: mcphttp.ModeSSEDisconnect})
	})
	sc.Step(`^I request a client_credentials token$`, w.requestClientCredentials)
	sc.Step(`^I initialize an MCP session$`, w.initialize)
	sc.Step(`^I list tools$`, w.listTools)
	sc.Step(`^I call tool "([^"]*)" with query "([^"]*)"$`, w.callTool)
	sc.Step(`^I refresh the token$`, w.refreshToken)
	sc.Step(`^the token endpoint recorded (\d+) request$`, w.tokenRecorded)
	sc.Step(`^the MCP server recorded (\d+) requests$`, w.mcpRecorded)
	sc.Step(`^subsequent MCP requests carry Mcp-Session-Id$`, w.sessionOnLaterRequests)
	sc.Step(`^the last MCP response is application/json$`, func() error {
		return w.contentTypeHas("application/json")
	})
	sc.Step(`^the last MCP response is text/event-stream$`, func() error {
		return w.contentTypeHas("text/event-stream")
	})
	sc.Step(`^the tool call succeeded$`, w.toolCallSucceeded)
	sc.Step(`^the initialize result is on the SSE stream$`, w.initializeOnSSE)
	sc.Step(`^the MCP server recorded the initialize body$`, w.recordedInitialize)
	sc.Step(`^the SSE stream is incomplete$`, w.sseIncomplete)
	sc.Step(`^the token expires in (\d+) second$`, w.tokenExpiresIn)
	sc.Step(`^a new refresh token is issued$`, w.newRefreshIssued)
	sc.Step(`^the old refresh token is rejected$`, w.oldRefreshRejected)
}

func (w *world) startToken(opts token.Options) error {
	if w.tok != nil {
		w.tok.Close()
	}
	w.tok = token.New(opts)
	return nil
}

func (w *world) startMCP(opts mcphttp.Options) error {
	if w.mcp != nil {
		w.mcp.Close()
	}
	w.mcp = mcphttp.New(opts)
	return nil
}

func (w *world) requestClientCredentials() error {
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {w.tok.ClientID()},
		"client_secret": {w.tok.ClientSecret()},
		"scope":         {"mcp.read"},
	}
	return w.postToken(form)
}

func (w *world) refreshToken() error {
	if w.tok == nil {
		return fmt.Errorf("no token endpoint")
	}
	w.oldRefresh = w.refresh
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {w.refresh},
		"client_id":     {w.tok.ClientID()},
		"client_secret": {w.tok.ClientSecret()},
	}
	return w.postToken(form)
}

func (w *world) postToken(form url.Values) error {
	resp, err := w.client.Post(w.tok.URL(), "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	w.lastStatus = resp.StatusCode
	w.lastCT = resp.Header.Get("Content-Type")
	w.lastBody = body
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token status %d: %s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return err
	}
	w.access = tok.AccessToken
	w.refresh = tok.RefreshToken
	w.expiresIn = tok.ExpiresIn
	if w.access == "" {
		return fmt.Errorf("empty access_token")
	}
	return nil
}

func (w *world) initialize() error {
	return w.mcpPost(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "harness", "version": "0"},
		},
	})
}

func (w *world) listTools() error {
	return w.mcpPost(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
}

func (w *world) callTool(name, query string) error {
	return w.mcpPost(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": map[string]any{"query": query},
		},
	})
}

func (w *world) mcpPost(payload map[string]any) error {
	if w.mcp == nil {
		return fmt.Errorf("no mcp server")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, w.mcp.URL(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if w.access != "" {
		req.Header.Set("Authorization", "Bearer "+w.access)
	}
	if w.session != "" {
		req.Header.Set("Mcp-Session-Id", w.session)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		w.lastErr = err
		w.lastBody = nil
		w.lastHeader = nil
		w.lastStatus = 0
		w.lastCT = ""
		return nil
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	w.lastErr = readErr
	w.lastBody = body
	w.lastStatus = resp.StatusCode
	w.lastCT = resp.Header.Get("Content-Type")
	w.lastHeader = resp.Header.Clone()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		w.session = sid
	}
	return nil
}

func (w *world) tokenRecorded(n int) error {
	got := len(w.tok.Requests())
	if got != n {
		return fmt.Errorf("token requests %d, want %d", got, n)
	}
	return nil
}

func (w *world) mcpRecorded(n int) error {
	got := len(w.mcp.Requests())
	if got != n {
		return fmt.Errorf("mcp requests %d, want %d", got, n)
	}
	return nil
}

func (w *world) sessionOnLaterRequests() error {
	reqs := w.mcp.Requests()
	if len(reqs) < 2 {
		return fmt.Errorf("need at least 2 mcp requests")
	}
	if w.session == "" {
		return fmt.Errorf("empty session id")
	}
	for i, rec := range reqs[1:] {
		if rec.Header.Get("Mcp-Session-Id") != w.session {
			return fmt.Errorf("request %d missing session", i+1)
		}
	}
	return nil
}

func (w *world) contentTypeHas(want string) error {
	if !strings.Contains(w.lastCT, want) {
		return fmt.Errorf("content-type %q, want %q", w.lastCT, want)
	}
	return nil
}

func (w *world) toolCallSucceeded() error {
	msg, err := w.rpcPayload()
	if err != nil {
		return err
	}
	result, _ := msg["result"].(map[string]any)
	if result == nil {
		return fmt.Errorf("no result: %v", msg)
	}
	if errFlag, ok := result["isError"].(bool); ok && errFlag {
		return fmt.Errorf("isError true")
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return fmt.Errorf("empty content")
	}
	first, _ := content[0].(map[string]any)
	if first["text"] != "hello" {
		return fmt.Errorf("text %v, want hello", first["text"])
	}
	return nil
}

func (w *world) initializeOnSSE() error {
	if !strings.Contains(w.lastCT, "text/event-stream") {
		return fmt.Errorf("content-type %q", w.lastCT)
	}
	msg, err := w.rpcPayload()
	if err != nil {
		return err
	}
	result, _ := msg["result"].(map[string]any)
	if result == nil {
		return fmt.Errorf("no result: %s", w.lastBody)
	}
	if result["protocolVersion"] == nil {
		return fmt.Errorf("missing protocolVersion")
	}
	if w.session == "" {
		return fmt.Errorf("missing Mcp-Session-Id")
	}
	return nil
}

func (w *world) recordedInitialize() error {
	reqs := w.mcp.Requests()
	if len(reqs) == 0 {
		return fmt.Errorf("no mcp requests")
	}
	var msg map[string]any
	if err := json.Unmarshal(reqs[0].Body, &msg); err != nil {
		return err
	}
	if msg["method"] != "initialize" {
		return fmt.Errorf("method %v", msg["method"])
	}
	return nil
}

func (w *world) sseIncomplete() error {
	if sseData(w.lastBody) != nil && json.Valid(sseData(w.lastBody)) {
		return fmt.Errorf("stream completed: %q", w.lastBody)
	}
	if len(w.lastBody) == 0 && w.lastErr == nil {
		return fmt.Errorf("empty body and no error")
	}
	return nil
}

func (w *world) tokenExpiresIn(n int) error {
	if w.expiresIn != n {
		return fmt.Errorf("expires_in %d, want %d", w.expiresIn, n)
	}
	return nil
}

func (w *world) newRefreshIssued() error {
	if w.refresh == "" {
		return fmt.Errorf("empty refresh")
	}
	if w.refresh == w.oldRefresh {
		return fmt.Errorf("refresh token was not rotated")
	}
	return nil
}

func (w *world) oldRefreshRejected() error {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {w.oldRefresh},
		"client_id":     {w.tok.ClientID()},
		"client_secret": {w.tok.ClientSecret()},
	}
	resp, err := w.client.Post(w.tok.URL(), "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return fmt.Errorf("old refresh accepted: %s", body)
	}
	var errBody map[string]any
	_ = json.Unmarshal(body, &errBody)
	if errBody["error"] != "invalid_grant" {
		return fmt.Errorf("error %v, want invalid_grant", errBody["error"])
	}
	return nil
}

func (w *world) rpcPayload() (map[string]any, error) {
	raw := w.lastBody
	if strings.Contains(w.lastCT, "text/event-stream") {
		data := sseData(w.lastBody)
		if data == nil {
			return nil, fmt.Errorf("no sse data in %q", w.lastBody)
		}
		raw = data
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func sseData(b []byte) []byte {
	for _, line := range bytes.Split(b, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if bytes.HasPrefix(line, []byte("data:")) {
			return bytes.TrimSpace(line[5:])
		}
	}
	return nil
}
