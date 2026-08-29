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

	"github.com/pmdroid/mcp-loadtester/internal/interp"
	"github.com/pmdroid/mcp-loadtester/internal/oauth"
	"github.com/pmdroid/mcp-loadtester/internal/scenario"
	"github.com/pmdroid/mcp-loadtester/internal/secret"
	"github.com/pmdroid/mcp-loadtester/internal/session"
)

type Config struct {
	Scenario           *scenario.Scenario
	SharedPool         bool
	InsecureLogSecrets bool
	LookupEnv          func(string) (string, bool)
	Stdout             io.Writer
	Stderr             io.Writer
}

type Summary struct {
	VUs            int           `json:"vus"`
	Duration       time.Duration `json:"duration"`
	Iterations     int64         `json:"iterations"`
	PeakSessions   int           `json:"peak_sessions"`
	SaturatedVUs   int           `json:"saturated_vus"`
	UniqueSessions int           `json:"unique_sessions"`
	HTTPClients    int           `json:"http_clients"`
	HTTPPool       string        `json:"http_pool"`
	SetupCount     int64         `json:"setup_count"`
	Errors         int64         `json:"errors"`
	SessionIDs     []string      `json:"-"`
}

func (s Summary) WriteText(w io.Writer) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "closed model\n")
	fmt.Fprintf(w, "vus: %d\n", s.VUs)
	fmt.Fprintf(w, "duration: %s\n", s.Duration)
	fmt.Fprintf(w, "iterations: %d\n", s.Iterations)
	fmt.Fprintf(w, "peak_sessions: %d\n", s.PeakSessions)
	fmt.Fprintf(w, "saturated_vus: %d\n", s.SaturatedVUs)
	fmt.Fprintf(w, "unique_sessions: %d\n", s.UniqueSessions)
	fmt.Fprintf(w, "http_pool: %s\n", s.HTTPPool)
	fmt.Fprintf(w, "http_clients: %d\n", s.HTTPClients)
	fmt.Fprintf(w, "setup: %d\n", s.SetupCount)
	fmt.Fprintf(w, "errors: %d\n", s.Errors)
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
		oa := sc.Auth.OAuth
		bound, err := oa.ClientSecret.Resolve(interp.Context{Secrets: lookup})
		if err != nil {
			return nil, err
		}
		rt, err := oa.RefreshToken.Resolve(interp.Context{Secrets: lookup})
		if err != nil {
			return nil, err
		}
		mgr, err := oauth.New(oauth.Config{
			Grant:              oa.Grant,
			TokenURL:           oa.TokenURL.Reveal(),
			ClientID:           oa.ClientID.Reveal(),
			ClientSecret:       secret.New("CLIENT_SECRET", bound.Reveal()),
			RefreshToken:       secret.New("REFRESH_TOKEN", rt.Reveal()),
			Scopes:             oa.Scopes,
			TokenScope:         oa.TokenScope,
			RefreshSkew:        oa.RefreshSkew,
			Warn:               cfg.Stderr,
			InsecureLogSecrets: cfg.InsecureLogSecrets,
			Client:             shared,
		})
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
		mu       sync.Mutex
		live     int
		peak     int
		ids      = map[string]struct{}{}
		sat      int
		iters    atomic.Int64
		setups   atomic.Int64
		errs     atomic.Int64
		wg       sync.WaitGroup
	)
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
	sum.WriteText(cfg.Stdout)
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
	open   func(string)
	close  func()
	iters  *atomic.Int64
	setups *atomic.Int64
	errs   *atomic.Int64
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
			_ = res
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
		headers[k] = rv.Reveal()
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
			if first == nil {
				first = &session.Error{Tag: session.TagProtocol, Err: err}
			}
			continue
		}
		intended := time.Now()
		res, err := sess.Call(ctx, st.Method, params, intended)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := checkExpect(st, res); err != nil {
			if first == nil {
				first = err
			}
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
