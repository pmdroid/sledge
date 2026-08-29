package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pmdroid/sledge/internal/session"
)

type Failures struct {
	Transport int64 `json:"transport"`
	Auth      int64 `json:"auth"`
	Protocol  int64 `json:"protocol"`
	Assertion int64 `json:"assertion"`
}

func (f Failures) Total() int64 {
	return f.Transport + f.Auth + f.Protocol + f.Assertion
}

type Snapshot struct {
	Failures   Failures
	Ops        int64
	Setups     int64
	Setup      Pair
	Operations map[string]Pair
	Tools      map[string]Pair
	P95        time.Duration
	ErrorRate  float64
}

type Collector struct {
	mu      sync.Mutex
	ops     map[string]*pairHist
	tools   map[string]*pairHist
	setup   *pairHist
	overall *Hist
	samples atomic.Int64
	setups  atomic.Int64
	trans   atomic.Int64
	auth    atomic.Int64
	proto   atomic.Int64
	assert  atomic.Int64
}

func NewCollector() *Collector {
	return &Collector{
		ops:     map[string]*pairHist{},
		tools:   map[string]*pairHist{},
		setup:   newPairHist(),
		overall: NewHist(),
	}
}

func (c *Collector) RecordSetup(intended, actual time.Duration, err error) {
	if c == nil {
		return
	}
	c.setups.Add(1)
	c.setup.record(intended, actual)
	c.tag(err)
}

func (c *Collector) Fail(err error) {
	if c == nil {
		return
	}
	c.tag(err)
}

func (c *Collector) RecordOp(method, tool string, intended, actual time.Duration, err error) {
	if c == nil {
		return
	}
	c.samples.Add(1)
	c.overall.Record(intended)
	c.mu.Lock()
	ph := c.ops[method]
	if ph == nil {
		ph = newPairHist()
		c.ops[method] = ph
	}
	if tool != "" {
		th := c.tools[tool]
		if th == nil {
			th = newPairHist()
			c.tools[tool] = th
		}
		th.record(intended, actual)
	}
	c.mu.Unlock()
	ph.record(intended, actual)
	c.tag(err)
}

func (c *Collector) tag(err error) {
	if err == nil {
		return
	}
	switch session.TagOf(err) {
	case session.TagAuth:
		c.auth.Add(1)
	case session.TagTransport:
		c.trans.Add(1)
	case session.TagAssertion:
		c.assert.Add(1)
	default:
		c.proto.Add(1)
	}
}

func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	fails := Failures{
		Transport: c.trans.Load(),
		Auth:      c.auth.Load(),
		Protocol:  c.proto.Load(),
		Assertion: c.assert.Load(),
	}
	ops := c.samples.Load()
	setups := c.setups.Load()
	den := ops + setups
	var rate float64
	if den > 0 {
		rate = float64(fails.Total()) / float64(den)
	}
	c.mu.Lock()
	operations := make(map[string]Pair, len(c.ops))
	for k, v := range c.ops {
		operations[k] = v.snapshot()
	}
	tools := make(map[string]Pair, len(c.tools))
	for k, v := range c.tools {
		tools[k] = v.snapshot()
	}
	c.mu.Unlock()
	setup := c.setup.snapshot()
	return Snapshot{
		Failures:   fails,
		Ops:        ops,
		Setups:     setups,
		Setup:      setup,
		Operations: operations,
		Tools:      tools,
		P95:        c.overall.Snapshot().P95,
		ErrorRate:  rate,
	}
}
