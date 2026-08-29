package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	Proto20260728 = "2026-07-28"
	Proto20251125 = "2025-11-25"

	RPCUnsupportedVersion = -32022
)

// SupportedVersions is newest first. Initialize offers the first entry, then
// retries with the newest version the server listed that we still know.
var SupportedVersions = []string{Proto20260728, Proto20251125}

func LatestVersion() string { return SupportedVersions[0] }

func SupportsVersion(v string) bool {
	for _, s := range SupportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

func PickVersion(server []string) string {
	have := make(map[string]struct{}, len(server))
	for _, s := range server {
		have[s] = struct{}{}
	}
	for _, s := range SupportedVersions {
		if _, ok := have[s]; ok {
			return s
		}
	}
	return ""
}

const (
	TagTransport = "transport"
	TagAuth      = "auth"
	TagProtocol  = "protocol"
	TagAssertion = "assertion"
)

type Error struct {
	Tag string
	Err error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Tag
	}
	return e.Tag + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func TagOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Tag
	}
	return ""
}

type Config struct {
	URL     string
	Headers map[string]string
	Client  *http.Client
	Auth    func(context.Context, *http.Request) error
}

type Result struct {
	ContentType     string
	Body            []byte
	Message         json.RawMessage
	Result          json.RawMessage
	RPCError        *RPCError
	IntendedLatency time.Duration
	ActualLatency   time.Duration
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message)
}

type Client struct {
	url     string
	headers map[string]string
	client  *http.Client
	auth    func(context.Context, *http.Request) error
	id      string
	nextID  int64
	proto   string
}

func New(cfg Config) *Client {
	cli := cfg.Client
	if cli == nil {
		cli = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	h := make(map[string]string, len(cfg.Headers))
	for k, v := range cfg.Headers {
		h[k] = v
	}
	return &Client{url: cfg.URL, headers: h, client: cli, auth: cfg.Auth, nextID: 1, proto: LatestVersion()}
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) Protocol() string {
	return c.version()
}

func (c *Client) version() string {
	if c.proto != "" {
		return c.proto
	}
	return LatestVersion()
}

func (c *Client) Initialize(ctx context.Context) (*Result, error) {
	ver := c.version()
	res, err := c.initOnce(ctx, ver)
	if err != nil {
		next := PickVersion(offeredVersions(res))
		if next == "" && ver == Proto20260728 && TagOf(err) == TagProtocol {
			next = Proto20251125
		}
		if next == "" || next == ver {
			return res, err
		}
		c.proto = next
		res, err = c.initOnce(ctx, next)
		if err != nil {
			return res, err
		}
	}
	got, nerr := negotiatedVersion(res.Result)
	if nerr != nil {
		return res, &Error{Tag: TagProtocol, Err: nerr}
	}
	if !SupportsVersion(got) {
		return res, &Error{Tag: TagProtocol, Err: fmt.Errorf("unsupported protocol version %q", got)}
	}
	c.proto = got
	return res, nil
}

func (c *Client) initOnce(ctx context.Context, ver string) (*Result, error) {
	return c.Call(ctx, "initialize", map[string]any{
		"protocolVersion": ver,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "sledge", "version": "0.0.0-dev"},
	}, time.Time{})
}

func (c *Client) Call(ctx context.Context, method string, params any, intended time.Time) (*Result, error) {
	if params == nil {
		params = map[string]any{}
	}
	params = c.attachMeta(params)
	id := c.nextID
	c.nextID++
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, &Error{Tag: TagProtocol, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, &Error{Tag: TagTransport, Err: err}
	}
	if err := c.applyHeaders(ctx, req); err != nil {
		return nil, err
	}
	c.applyMethodHeaders(req, method, params)
	actual := time.Now()
	if intended.IsZero() {
		intended = actual
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &Error{Tag: TagTransport, Err: err}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	done := time.Now()
	res := &Result{
		ContentType:     resp.Header.Get("Content-Type"),
		Body:            body,
		IntendedLatency: done.Sub(intended),
		ActualLatency:   done.Sub(actual),
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.id = sid
	}
	if readErr != nil {
		return res, &Error{Tag: TagTransport, Err: readErr}
	}
	statusErr := classifyStatus(resp.StatusCode)
	msg, rpcErr, perr := decodeRPC(res.ContentType, body, id)
	res.Message = msg
	res.RPCError = rpcErr
	if statusErr != nil {
		if rpcErr != nil {
			return res, &Error{Tag: TagProtocol, Err: rpcErr}
		}
		if perr != nil {
			return res, statusErr
		}
		return res, statusErr
	}
	if perr != nil {
		return res, perr
	}
	if rpcErr != nil {
		return res, &Error{Tag: TagProtocol, Err: rpcErr}
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return res, &Error{Tag: TagProtocol, Err: err}
	}
	res.Result = envelope.Result
	return res, nil
}

func (c *Client) Close(ctx context.Context) error {
	if c.id == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url, nil)
	if err != nil {
		return &Error{Tag: TagTransport, Err: err}
	}
	if err := c.applyHeaders(ctx, req); err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return &Error{Tag: TagTransport, Err: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := classifyStatus(resp.StatusCode); err != nil {
		return err
	}
	c.id = ""
	return nil
}

func (c *Client) applyHeaders(ctx context.Context, req *http.Request) error {
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", c.version())
	if req.Body != nil && req.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.id != "" {
		req.Header.Set("Mcp-Session-Id", c.id)
	}
	if c.auth != nil {
		if err := c.auth(ctx, req); err != nil {
			if TagOf(err) == "" {
				return &Error{Tag: TagAuth, Err: err}
			}
			return err
		}
	}
	return nil
}

func classifyStatus(code int) error {
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return &Error{Tag: TagAuth, Err: fmt.Errorf("http %d", code)}
	}
	if code >= 400 {
		return &Error{Tag: TagProtocol, Err: fmt.Errorf("http %d", code)}
	}
	if code != http.StatusOK && code != http.StatusAccepted && code != http.StatusNoContent {
		return &Error{Tag: TagProtocol, Err: fmt.Errorf("http %d", code)}
	}
	return nil
}

func (c *Client) applyMethodHeaders(req *http.Request, method string, params any) {
	if c.version() != Proto20260728 {
		return
	}
	req.Header.Set("Mcp-Method", method)
	if m, ok := params.(map[string]any); ok {
		if name, ok := m["name"].(string); ok && name != "" {
			req.Header.Set("Mcp-Name", name)
		}
	}
}

func (c *Client) attachMeta(params any) any {
	if c.version() != Proto20260728 {
		return params
	}
	out := map[string]any{}
	if m, ok := params.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	} else if params != nil {
		return params
	}
	out["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":     c.version(),
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo":          map[string]any{"name": "sledge", "version": "0.0.0-dev"},
	}
	return out
}

func offeredVersions(res *Result) []string {
	if res == nil || res.RPCError == nil || res.RPCError.Code != RPCUnsupportedVersion {
		return nil
	}
	var data struct {
		Supported []string `json:"supported"`
	}
	if err := json.Unmarshal(res.RPCError.Data, &data); err != nil {
		return nil
	}
	return data.Supported
}

func negotiatedVersion(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("initialize result missing protocolVersion")
	}
	var out struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.ProtocolVersion == "" {
		return "", errors.New("initialize result missing protocolVersion")
	}
	return out.ProtocolVersion, nil
}

func decodeRPC(ct string, body []byte, wantID int64) (json.RawMessage, *RPCError, error) {
	var raw []byte
	switch {
	case strings.Contains(ct, "text/event-stream"):
		msg, complete := sseJSON(body)
		if !complete {
			return nil, nil, &Error{Tag: TagTransport, Err: errors.New("incomplete SSE stream")}
		}
		raw = msg
	case strings.Contains(ct, "application/json"):
		raw = bytes.TrimSpace(body)
	default:
		return nil, nil, &Error{Tag: TagProtocol, Err: fmt.Errorf("unexpected content-type %q", ct)}
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, nil, &Error{Tag: TagProtocol, Err: errors.New("invalid json-rpc body")}
	}
	var env struct {
		ID    json.RawMessage `json:"id"`
		Error *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return raw, nil, &Error{Tag: TagProtocol, Err: err}
	}
	if len(env.ID) > 0 {
		var got int64
		if err := json.Unmarshal(env.ID, &got); err == nil && got != wantID {
			return raw, env.Error, &Error{Tag: TagProtocol, Err: fmt.Errorf("jsonrpc id %d, want %d", got, wantID)}
		}
	}
	return json.RawMessage(raw), env.Error, nil
}

func sseJSON(body []byte) ([]byte, bool) {
	norm := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	ended := bytes.HasSuffix(norm, []byte("\n\n"))
	parts := bytes.Split(norm, []byte("\n\n"))
	var last []byte
	for _, part := range parts {
		part = bytes.TrimRight(part, "\n")
		if len(part) == 0 {
			continue
		}
		var data [][]byte
		for _, line := range bytes.Split(part, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data:")) {
				data = append(data, bytes.TrimSpace(line[5:]))
			}
		}
		if len(data) == 0 {
			continue
		}
		last = bytes.Join(data, []byte("\n"))
	}
	if !ended {
		return last, false
	}
	return last, true
}
