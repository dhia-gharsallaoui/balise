package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests moved verbatim from internal/mcp/ratelimit_test.go when RateLimiter moved to
// this package (see ratelimit.go's doc comment for why). They prove Allow refuses the
// (perMin+1)th call in a window, that the window rolls (a caller refused now succeeds
// after simulated time passes, via the injected clock -- never a real
// time.Sleep(time.Minute)), that two keys never share a bucket, and that the limiter holds
// up under -race with concurrent callers. The one test that drove a real HTTP round trip
// through internal/mcp's authMiddleware (proving a 429 actually reaches a caller) stayed
// behind in internal/mcp/ratelimit_test.go, since it exercises mcp.NewHandler, not this
// package directly.

// TestRateLimiterRefusesTheNPlus1thCallInAWindow is judgment call (a): the (perMin+1)th
// call within one window must be refused, and every call up to perMin must still be
// allowed.
func TestRateLimiterRefusesTheNPlus1thCallInAWindow(t *testing.T) {
	rl := NewRateLimiter(3)

	for i := 0; i < 3; i++ {
		require.True(t, rl.Allow("tok-a"), "call %d of 3 should be allowed", i+1)
	}
	require.False(t, rl.Allow("tok-a"), "the 4th call in the same window must be refused")
}

// TestRateLimiterWindowRollsWithInjectedClock is judgment call (c): a caller refused in
// one window must succeed again once a full window has passed -- proved with a fake clock
// the test controls directly (ratelimit.go's now field, reachable because this package's
// tests are white-box), never a real time.Sleep(time.Minute).
func TestRateLimiterWindowRollsWithInjectedClock(t *testing.T) {
	rl := NewRateLimiter(2)
	current := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return current }

	require.True(t, rl.Allow("tok-a"))
	require.True(t, rl.Allow("tok-a"))
	require.False(t, rl.Allow("tok-a"), "the 3rd call in the same window must be refused")

	// Advance the fake clock by a full window (a hair past a minute, to stay clear of the
	// >= boundary by more than rounding error).
	current = current.Add(61 * time.Second)

	require.True(t, rl.Allow("tok-a"), "a new window must reset the count")
}

// TestRateLimiterTracksIndependentBucketsPerToken is judgment call (d): one key hitting
// its limit must never affect a different key's own bucket.
func TestRateLimiterTracksIndependentBucketsPerToken(t *testing.T) {
	rl := NewRateLimiter(1)

	require.True(t, rl.Allow("tok-a"))
	require.False(t, rl.Allow("tok-a"), "tok-a already used its one call this window")
	require.True(t, rl.Allow("tok-b"), "tok-b has its own, untouched bucket")
}

// TestRateLimiterIsRaceSafeUnderConcurrentCallers is judgment call (e): many goroutines
// calling Allow concurrently, across a handful of shared keys, must never trip the race
// detector (run this suite with -race, as VERIFY requires) and must never allow more than
// perMin calls through per key.
func TestRateLimiterIsRaceSafeUnderConcurrentCallers(t *testing.T) {
	const perMin = 50
	const callers = 500
	rl := NewRateLimiter(perMin)
	tokens := []string{"tok-a", "tok-b", "tok-c"}

	allowedPerToken := make(map[string]*int64, len(tokens))
	for _, tok := range tokens {
		var n int64
		allowedPerToken[tok] = &n
	}

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok := tokens[i%len(tokens)]
			if rl.Allow(tok) {
				atomic.AddInt64(allowedPerToken[tok], 1)
			}
		}(i)
	}
	wg.Wait()

	for _, tok := range tokens {
		got := atomic.LoadInt64(allowedPerToken[tok])
		require.LessOrEqualf(t, got, int64(perMin), "%s must never be allowed more than perMin calls in one window", tok)
		require.Greater(t, got, int64(0), "%s should have gotten at least one call through", tok)
	}
}
