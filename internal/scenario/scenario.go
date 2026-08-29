package scenario

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pmdroid/sledge/internal/interp"
	"gopkg.in/yaml.v3"
)

const TransportStreamableHTTP = "streamable-http"

type Options struct {
	VUs        int
	Duration   string
	SharedPool bool
	LookupEnv  func(string) (string, bool)
}

type Scenario struct {
	Version    int
	Target     Target
	Auth       *Auth
	HTTP       HTTP
	Workload   Workload
	Steps      []Step
	Vars       map[string]string
	Thresholds map[string]string
}

type Target struct {
	URL       interp.Value
	Transport string
	Headers   map[string]interp.Value
}

type Auth struct {
	OAuth *OAuth
}

type OAuth struct {
	Grant        string
	TokenURL     interp.Value
	ClientID     interp.Value
	ClientSecret interp.Value
	RefreshToken interp.Value
	Scopes       []string
	TokenScope   string
	RefreshSkew  time.Duration
}

type HTTP struct {
	Pool string
}

type Workload struct {
	Model      string
	VUs        int
	Duration   time.Duration
	RampUp     time.Duration
	ThinkTime  time.Duration
	Iterations int
	Session    Session
}

type Session struct {
	Mode string
}

type Step struct {
	Method string
	Body   map[string]any
	Expect *Expect
}

type Expect struct {
	OK          bool
	MaxDuration time.Duration
}

type file struct {
	Version    int               `yaml:"version"`
	Target     *targetFile       `yaml:"target"`
	Auth       *authFile         `yaml:"auth"`
	HTTP       *httpFile         `yaml:"http"`
	Workload   *workloadFile     `yaml:"workload"`
	Steps      []stepFile        `yaml:"steps"`
	Vars       map[string]string `yaml:"vars"`
	Thresholds map[string]string `yaml:"thresholds"`
}

type targetFile struct {
	URL       string            `yaml:"url"`
	Transport string            `yaml:"transport"`
	Headers   map[string]string `yaml:"headers"`
}

type authFile struct {
	OAuth *oauthFile `yaml:"oauth"`
}

type oauthFile struct {
	Grant        string       `yaml:"grant"`
	TokenURL     string       `yaml:"token_url"`
	ClientID     string       `yaml:"client_id"`
	ClientSecret string       `yaml:"client_secret"`
	RefreshToken string       `yaml:"refresh_token"`
	Scopes       []string     `yaml:"scopes"`
	TokenScope   string       `yaml:"token_scope"`
	RefreshSkew  yamlDuration `yaml:"refresh_skew"`
}

type httpFile struct {
	Pool string `yaml:"pool"`
}

type workloadFile struct {
	Model       string       `yaml:"model"`
	VUs         int          `yaml:"vus"`
	Duration    yamlDuration `yaml:"duration"`
	RampUp      yamlDuration `yaml:"ramp_up"`
	ThinkTime   yamlDuration `yaml:"think_time"`
	Iterations  int          `yaml:"iterations"`
	Session     sessionFile  `yaml:"session"`
	ArrivalRate *yaml.Node   `yaml:"arrival_rate"`
	Rate        *yaml.Node   `yaml:"rate"`
}

type sessionFile struct {
	Mode string `yaml:"mode"`
}

type stepFile struct {
	Method string
	Body   map[string]any
	Expect *expectFile
}

type expectFile struct {
	OK          bool         `yaml:"ok"`
	MaxDuration yamlDuration `yaml:"max_duration"`
}

type yamlDuration time.Duration

func (d *yamlDuration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Value == "" {
		*d = 0
		return nil
	}
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	pd, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q", s)
	}
	*d = yamlDuration(pd)
	return nil
}

func (s *stepFile) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("step must be a mapping")
	}
	var method string
	var body map[string]any
	var expect *expectFile
	for i := 0; i < len(n.Content); i += 2 {
		key := n.Content[i].Value
		val := n.Content[i+1]
		if key == "expect" {
			var e expectFile
			if err := val.Decode(&e); err != nil {
				return err
			}
			expect = &e
			continue
		}
		if method != "" {
			return fmt.Errorf("step has multiple methods")
		}
		method = key
		if err := val.Decode(&body); err != nil {
			return err
		}
		if body == nil {
			body = map[string]any{}
		}
	}
	if method == "" {
		return fmt.Errorf("step missing method")
	}
	s.Method = method
	s.Body = body
	s.Expect = expect
	return nil
}

func ValidateFile(path string, opts Options) error {
	_, err := LoadFile(path, opts)
	return err
}

func LoadFile(path string, opts Options) (*Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f, opts)
}

func Load(r io.Reader, opts Options) (*Scenario, error) {
	dec := yaml.NewDecoder(r)
	var raw file
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse scenario: %w", err)
	}
	if err := applyOverrides(&raw, opts); err != nil {
		return nil, err
	}
	if err := checkRequired(&raw); err != nil {
		return nil, err
	}
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	vars := map[string]string{}
	for k, v := range raw.Vars {
		iv, err := interpField(v, "vars."+k, false, interp.Context{Env: lookup})
		if err != nil {
			return nil, err
		}
		vars[k] = iv.String()
	}
	ctx := interp.Context{Env: lookup, Vars: vars}
	sc := &Scenario{
		Version:    raw.Version,
		Vars:       vars,
		Thresholds: raw.Thresholds,
	}
	url, err := interpField(raw.Target.URL, "target.url", false, ctx)
	if err != nil {
		return nil, err
	}
	sc.Target.URL = url
	sc.Target.Transport = raw.Target.Transport
	if len(raw.Target.Headers) > 0 {
		sc.Target.Headers = map[string]interp.Value{}
		for k, v := range raw.Target.Headers {
			hv, err := interpField(v, "target.headers."+k, true, ctx)
			if err != nil {
				return nil, err
			}
			sc.Target.Headers[k] = hv
		}
	}
	if raw.Auth != nil && raw.Auth.OAuth != nil {
		oa, err := loadOAuth(raw.Auth.OAuth, ctx)
		if err != nil {
			return nil, err
		}
		sc.Auth = &Auth{OAuth: oa}
	}
	if raw.HTTP != nil {
		sc.HTTP.Pool = raw.HTTP.Pool
	}
	if sc.HTTP.Pool == "" {
		sc.HTTP.Pool = "vu"
	}
	sc.Workload = Workload{
		Model:      raw.Workload.Model,
		VUs:        raw.Workload.VUs,
		Duration:   time.Duration(raw.Workload.Duration),
		RampUp:     time.Duration(raw.Workload.RampUp),
		ThinkTime:  time.Duration(raw.Workload.ThinkTime),
		Iterations: raw.Workload.Iterations,
		Session: Session{
			Mode: raw.Workload.Session.Mode,
		},
	}
	if sc.Workload.Session.Mode == "" {
		sc.Workload.Session.Mode = "per_vu"
	}
	for i, st := range raw.Steps {
		body, err := interpAny(st.Body, fmt.Sprintf("steps[%d]", i), false, ctx)
		if err != nil {
			return nil, err
		}
		bm, _ := body.(map[string]any)
		if bm == nil {
			bm = map[string]any{}
		}
		step := Step{Method: st.Method, Body: bm}
		if st.Expect != nil {
			step.Expect = &Expect{
				OK:          st.Expect.OK,
				MaxDuration: time.Duration(st.Expect.MaxDuration),
			}
		}
		sc.Steps = append(sc.Steps, step)
	}
	if err := interpMap(raw.Thresholds, "thresholds", false, ctx); err != nil {
		return nil, err
	}
	if err := validateLoaded(sc); err != nil {
		return nil, err
	}
	return sc, nil
}

func applyOverrides(raw *file, opts Options) error {
	if opts.VUs == 0 && opts.Duration == "" && !opts.SharedPool {
		return nil
	}
	if raw.Workload == nil {
		raw.Workload = &workloadFile{}
	}
	if opts.VUs != 0 {
		raw.Workload.VUs = opts.VUs
	}
	if opts.Duration != "" {
		d, err := time.ParseDuration(opts.Duration)
		if err != nil {
			return fmt.Errorf("invalid --duration %q", opts.Duration)
		}
		raw.Workload.Duration = yamlDuration(d)
	}
	if opts.SharedPool {
		if raw.HTTP == nil {
			raw.HTTP = &httpFile{}
		}
		raw.HTTP.Pool = "shared"
	}
	return nil
}

func loadOAuth(raw *oauthFile, ctx interp.Context) (*OAuth, error) {
	tokenURL, err := interpField(raw.TokenURL, "auth.oauth.token_url", true, ctx)
	if err != nil {
		return nil, err
	}
	clientID, err := interpField(raw.ClientID, "auth.oauth.client_id", true, ctx)
	if err != nil {
		return nil, err
	}
	clientSecret, err := interpField(raw.ClientSecret, "auth.oauth.client_secret", true, ctx)
	if err != nil {
		return nil, err
	}
	refresh, err := interpField(raw.RefreshToken, "auth.oauth.refresh_token", true, ctx)
	if err != nil {
		return nil, err
	}
	oa := &OAuth{
		Grant:        raw.Grant,
		TokenURL:     tokenURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refresh,
		Scopes:       raw.Scopes,
		TokenScope:   raw.TokenScope,
		RefreshSkew:  time.Duration(raw.RefreshSkew),
	}
	if oa.TokenScope == "" {
		oa.TokenScope = "shared"
	}
	if oa.TokenScope == "per-vu" {
		oa.TokenScope = "per_vu"
	}
	if oa.RefreshSkew == 0 {
		oa.RefreshSkew = 30 * time.Second
	}
	return oa, nil
}

func interpField(s, path string, allowSecret bool, ctx interp.Context) (interp.Value, error) {
	if s == "" {
		return interp.Value{}, nil
	}
	v, err := interp.Parse(s)
	if err != nil {
		return interp.Value{}, fmt.Errorf("%s: %w", path, err)
	}
	if v.HasSecret() && !allowSecret {
		return interp.Value{}, fmt.Errorf("secret interpolation is not allowed in %s", path)
	}
	return v.Resolve(ctx)
}

func interpAny(v any, path string, allowSecret bool, ctx interp.Context) (any, error) {
	switch t := v.(type) {
	case string:
		iv, err := interpField(t, path, allowSecret, ctx)
		if err != nil {
			return nil, err
		}
		return iv.String(), nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			cv, err := interpAny(child, path+"."+k, allowSecret, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = cv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			cv, err := interpAny(child, fmt.Sprintf("%s[%d]", path, i), allowSecret, ctx)
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

func interpMap(m map[string]string, path string, allowSecret bool, ctx interp.Context) error {
	for k, v := range m {
		if _, err := interpField(v, path+"."+k, allowSecret, ctx); err != nil {
			return err
		}
	}
	return nil
}

func checkRequired(raw *file) error {
	if raw.Version == 0 {
		return fmt.Errorf("missing required field: version")
	}
	if raw.Version != 1 {
		return fmt.Errorf("unsupported version %d", raw.Version)
	}
	if raw.Target == nil {
		return fmt.Errorf("missing required field: target")
	}
	if raw.Target.URL == "" {
		return fmt.Errorf("missing required field: target.url")
	}
	if raw.Target.Transport == "" {
		return fmt.Errorf("missing required field: target.transport")
	}
	if raw.Workload == nil {
		return fmt.Errorf("missing required field: workload")
	}
	if raw.Workload.ArrivalRate != nil || raw.Workload.Rate != nil {
		return fmt.Errorf("arrival-rate is reserved, not implemented")
	}
	if raw.Workload.Model == "" {
		return fmt.Errorf("missing required field: workload.model")
	}
	if raw.Workload.Model == "arrival-rate" || raw.Workload.Model == "open" {
		return fmt.Errorf("arrival-rate is reserved, not implemented")
	}
	if raw.Workload.Duration == 0 {
		return fmt.Errorf("missing required field: workload.duration")
	}
	if raw.Workload.VUs == 0 {
		return fmt.Errorf("missing required field: workload.vus")
	}
	if len(raw.Steps) == 0 {
		return fmt.Errorf("missing required field: steps")
	}
	return nil
}

func validateLoaded(sc *Scenario) error {
	if sc.Target.Transport != TransportStreamableHTTP {
		return fmt.Errorf("unknown transport %q; only %s is legal", sc.Target.Transport, TransportStreamableHTTP)
	}
	if sc.Workload.Model != "closed" {
		return fmt.Errorf("arrival-rate is reserved, not implemented")
	}
	if sc.Workload.VUs <= 0 {
		return fmt.Errorf("workload.vus must be > 0")
	}
	if sc.Workload.Duration <= 0 {
		return fmt.Errorf("workload.duration must be > 0")
	}
	if sc.Workload.Iterations < 0 {
		return fmt.Errorf("workload.iterations must be >= 0")
	}
	switch sc.HTTP.Pool {
	case "vu", "shared":
	default:
		return fmt.Errorf("unknown http.pool %q", sc.HTTP.Pool)
	}
	switch sc.Workload.Session.Mode {
	case "per_vu", "per_iteration":
	default:
		return fmt.Errorf("unknown session.mode %q", sc.Workload.Session.Mode)
	}
	if sc.Auth != nil && sc.Auth.OAuth != nil {
		oa := sc.Auth.OAuth
		if oa.Grant == "" {
			return fmt.Errorf("missing required field: auth.oauth.grant")
		}
		switch oa.Grant {
		case "client_credentials", "refresh_token":
		default:
			return fmt.Errorf("unknown oauth grant %q", oa.Grant)
		}
		if oa.TokenURL.String() == "" {
			return fmt.Errorf("missing required field: auth.oauth.token_url")
		}
		if oa.ClientID.String() == "" {
			return fmt.Errorf("missing required field: auth.oauth.client_id")
		}
		if oa.Grant == "client_credentials" && oa.ClientSecret.String() == "" && !oa.ClientSecret.HasSecret() {
			return fmt.Errorf("missing required field: auth.oauth.client_secret")
		}
		if oa.Grant == "refresh_token" && oa.RefreshToken.String() == "" && !oa.RefreshToken.HasSecret() {
			return fmt.Errorf("missing required field: auth.oauth.refresh_token")
		}
		switch oa.TokenScope {
		case "shared", "per_vu":
		default:
			return fmt.Errorf("unknown token_scope %q", oa.TokenScope)
		}
	}
	return nil
}
