package compile_test

import (
	"testing"
	"time"

	"github.com/dhia/balise/internal/compile"
	"github.com/dhia/balise/internal/store"
	"github.com/stretchr/testify/require"
)

func TestLoadStateOnAFreshVaultHasNoPages(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	state, err := compile.LoadState(pages)
	require.NoError(t, err)
	require.Empty(t, state.Pages)
	require.False(t, state.Unchanged("work/gotcha/foo.md", "anyhash"))
}

func TestWithRecordedMarksAPageUnchangedAtTheSameHash(t *testing.T) {
	state := compile.State{Pages: map[string]compile.PageState{}}
	updated := state.WithRecorded("work/gotcha/foo.md", "hash-a", time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))

	require.True(t, updated.Unchanged("work/gotcha/foo.md", "hash-a"))
	require.False(t, updated.Unchanged("work/gotcha/foo.md", "hash-b"), "a changed body hash must not be treated as unchanged")
	require.False(t, updated.Unchanged("work/gotcha/other.md", "hash-a"), "an unrelated path must not be treated as unchanged")
}

func TestWithRecordedDoesNotMutateTheReceiver(t *testing.T) {
	original := compile.State{Pages: map[string]compile.PageState{}}
	_ = original.WithRecorded("work/gotcha/foo.md", "hash-a", time.Now())

	require.Empty(t, original.Pages, "WithRecorded must return a new State, not mutate the original")
}

func TestStateRoundTripsThroughRenderAndLoadState(t *testing.T) {
	pages, err := store.InitGit(t.TempDir())
	require.NoError(t, err)

	state := compile.State{Pages: map[string]compile.PageState{}}
	state = state.WithRecorded("work/gotcha/foo.md", "hash-a", time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))

	rendered, err := state.Render()
	require.NoError(t, err)

	_, err = pages.Write(".balise/compile/extract-claims-state.json", rendered, "")
	require.NoError(t, err)

	reloaded, err := compile.LoadState(pages)
	require.NoError(t, err)
	require.True(t, reloaded.Unchanged("work/gotcha/foo.md", "hash-a"))
	require.False(t, reloaded.Unchanged("work/gotcha/foo.md", "hash-b"))
}
