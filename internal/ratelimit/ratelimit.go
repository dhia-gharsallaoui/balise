// Package ratelimit is a small, dependency-free, in-process rate limiter shared by
// internal/mcp (per-agent-token limits on MCP calls) and internal/api (per-client-address
// limits on owner login attempts). It started life as internal/mcp/ratelimit.go; it moved
// here for the same reason vault.ValidateSegment moved to internal/vault when both
// internal/store and internal/mcp needed it -- internal/store must not import internal/mcp,
// and duplicating the limiter would let the two copies drift. internal/ratelimit imports
// nothing else in this module, so both internal/api and internal/mcp can depend on it with
// no risk of an import cycle.
package ratelimit

import (
	"sync"
	"time"
)

// DefaultPerMinute is the fallback used when a caller configures <= 0: 04 section 12's
// stated default for MCP calls, 60 per token per minute. Callers with a different natural
// default (e.g. owner login attempts) should pass their own perMinute explicitly rather
// than relying on this constant.
const DefaultPerMinute = 60

// RateLimiter is an in-process, per-key, fixed-window rate limiter. There is no shared
// store across balise processes -- if this API or MCP server is ever run as more than one
// instance behind a load balancer, each instance enforces its own independent limit, not a
// combined one. That is an accepted limitation for this deployment (a single process on
// 127.0.0.1), stated here and in the completion report rather than left implicit; there is
// no golang.org/x/time/rate (or any other rate-limiting dependency) in go.mod, so this is
// hand-rolled from sync and time only.
type RateLimiter struct {
	mu       sync.Mutex
	perMin   int
	window   time.Duration
	counters map[string]*keyWindow
	// now is the limiter's clock, called at the start of every Allow. Defaulted to
	// time.Now by NewRateLimiter -- production code never sets it to anything else. Its
	// only reason to exist is so a test in this package can substitute a fake,
	// test-controlled clock to prove the window actually rolls (a caller refused now
	// succeeds a simulated minute later) without a real time.Sleep(time.Minute) in the
	// suite.
	now func() time.Time
}

type keyWindow struct {
	start time.Time
	count int
}

// NewRateLimiter builds a limiter allowing perMinute calls per key per rolling minute.
// perMinute <= 0 falls back to DefaultPerMinute, so a caller misconfiguring this to zero
// cannot accidentally lock every key out entirely.
func NewRateLimiter(perMinute int) *RateLimiter {
	if perMinute <= 0 {
		perMinute = DefaultPerMinute
	}
	return &RateLimiter{
		perMin:   perMinute,
		window:   time.Minute,
		counters: map[string]*keyWindow{},
		now:      time.Now,
	}
}

// Allow reports whether key may make one more call right now, and records the call if so.
// It is a fixed-window counter (reset once per rolling minute measured from that key's
// first call inside the current window), not a precise sliding log or token bucket: O(1)
// per call, bounded memory (one entry per distinct key ever seen), and more than adequate
// for a 60/min (or similar) default. The accepted tradeoff is that a caller timed right at
// a window boundary can burst up to perMin calls twice in quick succession -- never more
// than double the configured rate, and never unbounded.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	w, ok := rl.counters[key]
	if !ok || now.Sub(w.start) >= rl.window {
		rl.counters[key] = &keyWindow{start: now, count: 1}
		return true
	}
	if w.count >= rl.perMin {
		return false
	}
	w.count++
	return true
}
