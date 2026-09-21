package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelatedRejectsUnauthenticatedCall(t *testing.T) {
	d := newTestDeps(t)
	_, _, err := d.related(context.Background(), reqWithToken("bogus"), RelatedInput{Scope: "work", Slug: "widget"})
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestRelatedRequiresReadCapability(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "writer-only", []string{"work"}, []string{"remember"})

	_, _, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "widget"})
	require.Error(t, err)
}

func TestRelatedReturnsOutboundGroupedByKindAndBacklinks(t *testing.T) {
	d := newTestDeps(t)
	a := seedPageFull(t, d.pool, "work", "general", "note", "general claim", 10, false)
	b := seedPageFull(t, d.pool, "work", "specific", "note", "specific claim", 10, false)
	seedEdge(t, d.pool, "work", b, a, "specializes")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "specific"})
	require.NoError(t, err)
	require.Len(t, out.Outbound["specializes"], 1)
	require.Equal(t, "general", out.Outbound["specializes"][0].Slug)
	require.Empty(t, out.Backlinks)

	_, outBack, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "general"})
	require.NoError(t, err)
	require.Len(t, outBack.Backlinks, 1)
	require.Equal(t, "specific", outBack.Backlinks[0].Slug)
}

// TestRelatedRefusesScopeOutsideToken is the hostile cross-scope input: a
// scope the token does not hold reads identically to a nonexistent slug --
// errNotFound, never a distinguishable error and never any data.
func TestRelatedRefusesScopeOutsideToken(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "client-globex", Slug: "secret"})
	require.ErrorIs(t, err, errNotFound)
	require.Empty(t, out.Outbound)
	require.Empty(t, out.Backlinks)
}

func TestRelatedUnknownSlugWithinScopeIsNotFound(t *testing.T) {
	d := newTestDeps(t)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, _, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "does-not-exist"})
	require.ErrorIs(t, err, errNotFound)
}

// TestRelatedNeverDistinguishesExistsElsewhere pins 04 section 12/14's "never
// distinguish 'exists elsewhere'", the same invariant fetch_test.go pins for
// fetch: a real slug outside the token's scopes and a wholly nonexistent
// slug must produce byte-for-byte the same error.
func TestRelatedNeverDistinguishesExistsElsewhere(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "client-globex", "secret", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, _, errOutOfScope := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "client-globex", Slug: "secret"})
	_, _, errNonexistent := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "totally-made-up"})

	require.Error(t, errOutOfScope)
	require.Error(t, errNonexistent)
	require.Equal(t, errOutOfScope.Error(), errNonexistent.Error())
}

// TestRelatedDepthIsClampedToMax is a hostile-input depth bomb: a caller
// asking for an enormous depth must never turn into an enormous (or
// unbounded) traversal -- depth is silently clamped to maxRelatedDepth, and
// the clamped value is echoed back so the caller can tell.
func TestRelatedDepthIsClampedToMax(t *testing.T) {
	d := newTestDeps(t)
	seedPageFull(t, d.pool, "work", "start", "note", "start claim", 10, false)
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "start", Depth: 1_000_000})
	require.NoError(t, err)
	require.Equal(t, maxRelatedDepth, out.Depth)
}

// TestRelatedSurvivesCyclicGraphDepthBomb is the depth-bomb hostile input
// 04 section 12 calls out explicitly: a cyclic graph (a -> b -> a) driven
// with the maximum depth must return promptly, without hanging and without
// growing without bound, because the BFS's visited map stops it from ever
// revisiting a node.
func TestRelatedSurvivesCyclicGraphDepthBomb(t *testing.T) {
	d := newTestDeps(t)
	a := seedPageFull(t, d.pool, "work", "a", "note", "a claim", 10, false)
	b := seedPageFull(t, d.pool, "work", "b", "note", "b claim", 10, false)
	seedEdge(t, d.pool, "work", a, b, "specializes")
	seedEdge(t, d.pool, "work", b, a, "specializes")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	done := make(chan error, 1)
	go func() {
		_, out, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "a", Depth: maxRelatedDepth})
		if err == nil {
			require.LessOrEqual(t, len(out.Outbound["specializes"]), 2)
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("related hung on a cyclic graph instead of terminating via its visited-node cap")
	}
}

// TestRelatedDanglingEdgeDoesNotCrash: an edge whose to_uid names no
// document at all (a stale or corrupt edge row) must be silently excluded,
// never a crash and never surfaced as an error -- the same contract
// GetOutboundNeighbors's own doc comment states.
func TestRelatedDanglingEdgeDoesNotCrash(t *testing.T) {
	d := newTestDeps(t)
	a := seedPageFull(t, d.pool, "work", "a", "note", "a claim", 10, false)
	seedEdge(t, d.pool, "work", a, "work-does-not-exist", "specializes")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	_, out, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "a"})
	require.NoError(t, err)
	require.Empty(t, out.Outbound["specializes"])
}

func TestRelatedRecordsAnAuditRowOnEveryCall(t *testing.T) {
	d := newTestDeps(t)
	seedPage(t, d.pool, "work", "widget", "claim text")
	raw := mintToken(t, d.pool, "claude-code", []string{"work"}, []string{"read"})

	before := countAuditRows(t, d.pool)
	_, _, err := d.related(context.Background(), reqWithToken(raw), RelatedInput{Scope: "work", Slug: "widget"})
	require.NoError(t, err)
	require.Equal(t, before+1, countAuditRows(t, d.pool))
}
