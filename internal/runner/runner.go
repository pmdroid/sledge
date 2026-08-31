package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pmdroid/sledge/internal/interp"
	"github.com/pmdroid/sledge/internal/mcpoauth"
	"github.com/pmdroid/sledge/internal/metrics"
	"github.com/pmdroid/sledge/internal/oauth"
	"github.com/pmdroid/sledge/internal/redact"
	"github.com/pmdroid/sledge/internal/scenario"
	"github.com/pmdroid/sledge/internal/secret"
	"github.com/pmdroid/sledge/internal/session"
	"github.com/pmdroid/sledge/internal/termstyle"
)

type Config struct {
	Scenario           *scenario.Scenario
	SharedPool         bool
	InsecureLogSecrets bool
	LookupEnv          func(string) (string, bool)
	Stdout             io.Writer
	Stderr             io.Writer
	JSON               io.Writer
	OutPath            string
	Progress           bool
	Color              termstyle.Mode
	Log                *redact.Logger
}

type Summary struct {
	VUs            int                     `json:"vus"`
	Duration       time.Duration           `json:"duration"`
	Iterations     int64                   `json:"iterations"`
	PeakSessions   int                     `json:"peak_sessions"`
	SaturatedVUs   int                     `json:"saturated_vus"`
	UniqueSessions int                     `json:"unique_sessions"`
	HTTPClients    int                     `json:"http_clients"`
	HTTPPool       string                  `json:"http_pool"`
	SetupCount     int64                   `json:"setup_count"`
	Errors         int64                   `json:"errors"`
	ThroughputRPS  float64                 `json:"throughput_rps"`
	ErrorRate      float64                 `json:"error_rate"`
	Failures       metrics.Failures        `json:"failures"`
	P95Latency     time.Duration           `json:"-"`
	Setup          metrics.Pair            `json:"setup"`
	Operations     map[string]metrics.Pair `json:"operations"`
	Tools          map[string]metrics.Pair `json:"tools"`
	Thresholds     []metrics.Result        `json:"thresholds"`
	Failed         bool                    `json:"failed"`
	JSON           []byte                  `json:"-"`
	SessionIDs     []string                `json:"-"`
}

func Run(ctx context.Context, cfg Config) (*Summary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sc := cfg.Scenario
	if sc == nil {
		return nil, fmt.Errorf("missing scenario")
	}
	lookup := cfg.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	log := cfg.Log
	if log == nil {
		log = redact.New(cfg.InsecureLogSecrets)
	}
	coll := metrics.NewCollector()
	pool := sc.HTTP.Pool
	if cfg.SharedPool {
		pool = "shared"
	}
	if pool == "" {
		pool = "vu"
	}
	var shared *http.Client
	clients := sc.Workload.VUs
	if pool == "shared" {
		shared = newHTTPClient()
		clients = 1
	}
	var tok *oauth.Manager
	if sc.Auth != nil && sc.Auth.OAuth != nil {
		mgr, err := newOAuth(sc, lookup, log, cfg.Stderr, shared)
		if err != nil {
			return nil, err
		}
		tok = mgr
	}
	runCtx, cancel := context.WithTimeout(ctx, sc.Workload.Duration)
	defer cancel()
	start := time.Now()
	sum := &Summary{
		VUs:         sc.Workload.VUs,
		HTTPClients: clients,
		HTTPPool:    pool,
	}
	var (
		mu     sync.Mutex
		live   int
		peak   int
		ids    = map[string]struct{}{}
		sat    int
		iters  atomic.Int64
		setups atomic.Int64
		errs   atomic.Int64
		wg     sync.WaitGroup
	)
	done := make(chan struct{})
	stdoutSty := termstyle.New(cfg.Stdout, cfg.Color)
	stderrSty := termstyle.New(cfg.Stderr, cfg.Color)
	if cfg.Progress {
		go runProgress(cfg.Stderr, start, &iters, coll, done, stderrSty)
	}
	trackOpen := func(id string) {
		mu.Lock()
		if id != "" {
			ids[id] = struct{}{}
		}
		live++
		if live > peak {
			peak = live
		}
		mu.Unlock()
	}
	trackClose := func() {
		mu.Lock()
		if live > 0 {
			live--
		}
		mu.Unlock()
	}
	for i := 0; i < sc.Workload.VUs; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := rampWait(runCtx, id, sc.Workload.VUs, sc.Workload.RampUp); err != nil {
				return
			}
			saturated := runVU(runCtx, vuOpts{
				id:     id,
				sc:     sc,
				shared: shared,
				tok:    tok,
				lookup: lookup,
				log:    log,
				coll:   coll,
				open:   trackOpen,
				close:  trackClose,
				iters:  &iters,
				setups: &setups,
				errs:   &errs,
			})
			if saturated {
				mu.Lock()
				sat++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	close(done)
	if cfg.Progress && cfg.Stderr != nil {
		writeProgressLine(cfg.Stderr, start, &iters, coll, stderrSty)
		fmt.Fprintln(cfg.Stderr)
	}
	sum.Duration = time.Since(start)
	sum.Iterations = iters.Load()
	sum.SetupCount = setups.Load()
	sum.Errors = errs.Load()
	mu.Lock()
	sum.PeakSessions = peak
	sum.SaturatedVUs = sat
	sum.UniqueSessions = len(ids)
	sum.SessionIDs = make([]string, 0, len(ids))
	for id := range ids {
		sum.SessionIDs = append(sum.SessionIDs, id)
	}
	mu.Unlock()
	snap := coll.Snapshot()
	sum.Failures = snap.Failures
	sum.ErrorRate = snap.ErrorRate
	sum.P95Latency = snap.P95
	sum.Setup = snap.Setup
	sum.Operations = snap.Operations
	sum.Tools = snap.Tools
	if sum.Duration > 0 {
		sum.ThroughputRPS = float64(snap.Ops) / sum.Duration.Seconds()
	}
	th, err := metrics.Evaluate(sc.Thresholds, snap)
	if err != nil {
		return sum, &ConfigError{err: err}
	}
	sum.Thresholds = th
	sum.Failed = metrics.Failed(th)
	sum.WriteText(cfg.Stdout, stdoutSty)
	raw, err := sum.marshalJSON(log)
	if err != nil {
		return sum, err
	}
	if cfg.JSON != nil {
		if _, err := cfg.JSON.Write(raw); err != nil {
			return sum, err
		}
	}
	if err := writeOutFile(cfg.OutPath, raw); err != nil {
		return sum, err
	}
	return sum, nil
}

func rampWait(ctx context.Context, id, vus int, ramp time.Duration) error {
	if ramp <= 0 || vus <= 1 || id == 0 {
		return nil
	}
	delay := time.Duration(id) * ramp / time.Duration(vus)
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type vuOpts struct {
	id     int
	sc     *scenario.Scenario
	shared *http.Client
	tok    *oauth.Manager
	lookup func(string) (string, bool)
	log    *redact.Logger
	coll   *metrics.Collector
	open   func(string)
	close  func()
	iters  *atomic.Int64
	setups *atomic.Int64
	errs   *atomic.Int64
}

func newOAuth(sc *scenario.Scenario, lookup func(string) (string, bool), log *redact.Logger, stderr io.Writer, shared *http.Client) (*oauth.Manager, error) {
	oa := sc.Auth.OAuth
	bound, err := oa.ClientSecret.Resolve(interp.Context{Secrets: lookup})
	if err != nil {
		return nil, err
	}
	rt, err := oa.RefreshToken.Resolve(interp.Context{Secrets: lookup})
	if err != nil {
		return nil, err
	}
	cs := bound.Reveal()
	rts := rt.Reveal()
	tokenURL := oa.TokenURL.Reveal()
	clientID := oa.ClientID.Reveal()
	grant := oa.Grant
	if grant == oauth.GrantAuthCode || grant == oauth.GrantMCP {
		if rts == "" {
			rec, err := mcpoauth.Load(sc.Target.URL.Reveal())
			if err != nil {
				return nil, &ConfigError{err: err}
			}
			rts = rec.RefreshToken
			if tokenURL == "" {
				tokenURL = rec.TokenURL
			}
			if clientID == "" {
				clientID = rec.ClientID
			}
			log.Watch(rec.AccessToken)
		}
		grant = oauth.GrantRefresh
	}
	log.Watch(cs)
	log.Watch(rts)
	return oauth.New(oauth.Config{
		Grant:              grant,
		TokenURL:           tokenURL,
		ClientID:           clientID,
		ClientSecret:       secret.New("CLIENT_SECRET", cs),
		RefreshToken:       secret.New("REFRESH_TOKEN", rts),
		Scopes:             oa.Scopes,
		TokenScope:         oa.TokenScope,
		RefreshSkew:        oa.RefreshSkew,
		Warn:               stderr,
		InsecureLogSecrets: false,
		Log:                log,
		Client:             shared,
	})
}

func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 1024
	tr.MaxIdleConnsPerHost = 1024
	tr.IdleConnTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

func runVU(ctx context.Context, o vuOpts) bool {
	vu := strconv.Itoa(o.id)
	maxIter := o.sc.Workload.Iterations
	mode := o.sc.Workload.Session.Mode
	if mode == "" {
		mode = "per_vu"
	}
	think := o.sc.Workload.ThinkTime
	httpClient := o.shared
	if httpClient == nil {
		httpClient = newHTTPClient()
		defer httpClient.CloseIdleConnections()
	}
	var idle time.Duration
	iter := 0
	var sess *session.Client
	defer func() {
		if sess != nil {
			cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = sess.Close(cctx)
			cancel()
			o.close()
		}
	}()
	for {
		if ctx.Err() != nil {
			break
		}
		iter++
		if maxIter > 0 && iter > maxIter {
			break
		}
		iterS := strconv.Itoa(iter)
		if mode == "per_iteration" || sess == nil {
			if sess != nil {
				cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = sess.Close(cctx)
				cancel()
				o.close()
				sess = nil
			}
			sess = newClient(o, httpClient, vu, iterS)
			res, err := sess.Initialize(ctx)
			o.setups.Add(1)
			if o.coll != nil {
				in, ac := latencies(res)
				o.coll.RecordSetup(in, ac, err)
			}
			if err != nil {
				if ctx.Err() == nil {
					o.errs.Add(1)
				}
				cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = sess.Close(cctx)
				cancel()
				sess = nil
				if ctx.Err() != nil {
					break
				}
				idle += thinkWait(ctx, think)
				continue
			}
			o.open(sess.ID())
		}
		if err := runSteps(ctx, sess, o, vu, iterS); err != nil && ctx.Err() == nil {
			o.errs.Add(1)
		}
		o.iters.Add(1)
		if ctx.Err() != nil {
			break
		}
		if think > 0 {
			idle += thinkWait(ctx, think)
		}
	}
	return idle == 0 && iter > 0
}

func newClient(o vuOpts, httpClient *http.Client, vu, iter string) *session.Client {
	headers := map[string]string{}
	ctx := interp.Context{
		Env:       o.lookup,
		Vars:      o.sc.Vars,
		Secrets:   o.lookup,
		VU:        vu,
		Iteration: iter,
	}
	for k, v := range o.sc.Target.Headers {
		rv, err := v.Resolve(ctx)
		if err != nil {
			continue
		}
		val := rv.Reveal()
		headers[k] = val
		if o.log != nil {
			o.log.Watch(val)
		}
	}
	cfg := session.Config{
		URL:     o.sc.Target.URL.Reveal(),
		Headers: headers,
		Client:  httpClient,
	}
	if o.tok != nil {
		tok := o.tok
		id := vu
		cfg.Auth = func(ctx context.Context, req *http.Request) error {
			return tok.Inject(ctx, req, id)
		}
	}
	return session.New(cfg)
}

func runSteps(ctx context.Context, sess *session.Client, o vuOpts, vu, iter string) error {
	ictx := interp.Context{
		Env:       o.lookup,
		Vars:      o.sc.Vars,
		Secrets:   o.lookup,
		VU:        vu,
		Iteration: iter,
	}
	var first error
	for i, st := range o.sc.Steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		params, err := bindAny(st.Body, fmt.Sprintf("steps[%d]", i), ictx)
		if err != nil {
			berr := &session.Error{Tag: session.TagProtocol, Err: err}
			if o.coll != nil {
				o.coll.Fail(berr)
			}
			if first == nil {
				first = berr
			}
			continue
		}
		intended := time.Now()
		res, err := sess.Call(ctx, st.Method, params, intended)
		if err2 := checkExpect(st, res); err2 != nil {
			if err == nil {
				err = err2
			}
		}
		if o.coll != nil {
			in, ac := latencies(res)
			o.coll.RecordOp(st.Method, toolName(st.Method, params), in, ac, err)
		}
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
	}
	return first
}

func checkExpect(st scenario.Step, res *session.Result) error {
	if st.Expect == nil || res == nil {
		return nil
	}
	if st.Expect.MaxDuration > 0 && res.ActualLatency > st.Expect.MaxDuration {
		return &session.Error{Tag: session.TagAssertion, Err: fmt.Errorf("duration %s exceeds %s", res.ActualLatency, st.Expect.MaxDuration)}
	}
	if st.Expect.OK {
		if isToolError(res.Result) {
			return &session.Error{Tag: session.TagAssertion, Err: fmt.Errorf("tool isError")}
		}
	}
	return nil
}

func isToolError(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var wrap struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return false
	}
	return wrap.IsError
}

func latencies(res *session.Result) (time.Duration, time.Duration) {
	if res == nil {
		return 0, 0
	}
	return res.IntendedLatency, res.ActualLatency
}

func toolName(method string, params any) string {
	if method != "tools/call" {
		return ""
	}
	m, _ := params.(map[string]any)
	if m == nil {
		return ""
	}
	s, _ := m["name"].(string)
	return s
}

func thinkWait(ctx context.Context, d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	start := time.Now()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return time.Since(start)
	case <-t.C:
		return time.Since(start)
	}
}

func bindAny(v any, path string, ctx interp.Context) (any, error) {
	switch t := v.(type) {
	case string:
		iv, err := interp.Parse(t)
		if err != nil {
			return t, nil
		}
		rv, err := iv.Resolve(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return rv.String(), nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			cv, err := bindAny(child, path+"."+k, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = cv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			cv, err := bindAny(child, fmt.Sprintf("%s[%d]", path, i), ctx)
			if err != nil {
				return nil, err
			}
			out[i] = cv
		}
		return out, nil
	default:
		return v, nil
	}
}
