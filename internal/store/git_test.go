package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

// headPaths lists what HEAD's tree actually records, as opposed to what is on disk. A staged
// removal that only deleted the file would leave the vault's system of record still claiming
// a page that no longer exists, and the next checkout would resurrect it.
func headPaths(t *testing.T, s *store.GitPageStore) []string {
	t.Helper()
	repo, err := git.PlainOpen(s.Root())
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	tree, err := commit.Tree()
	require.NoError(t, err)

	paths := []string{}
	require.NoError(t, tree.Files().ForEach(func(file *object.File) error {
		paths = append(paths, file.Name)
		return nil
	}))
	sort.Strings(paths)
	return paths
}

func newStore(t *testing.T) *store.GitPageStore {
	t.Helper()
	s, err := store.InitGit(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestCommitThenReadRoundTrips(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("hello")}}, "Dhia <d@x>", "add a")
	require.NoError(t, err)

	data, version, err := s.Read("work/a.md")
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)
	require.Len(t, version, 40)
}

func TestCommitIsAtomicAcrossFiles(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: "work/a.md", Data: []byte("a")},
		{Path: "work/b.md", Data: []byte("b")},
	}, "Dhia <d@x>", "add two")
	require.NoError(t, err)

	history, err := s.History("work/a.md", 5)
	require.NoError(t, err)
	require.Len(t, history, 1, "both files land in one commit")
}

func TestWriteWithCorrectVersionSucceeds(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("one")}}, "Dhia <d@x>", "one")
	require.NoError(t, err)

	_, version, err := s.Read("work/a.md")
	require.NoError(t, err)
	_, err = s.Write("work/a.md", []byte("two"), version)
	require.NoError(t, err)

	data, _, err := s.Read("work/a.md")
	require.NoError(t, err)
	require.Equal(t, []byte("two"), data)
}

func TestWriteWithStaleVersionConflicts(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("one")}}, "Dhia <d@x>", "one")
	require.NoError(t, err)

	_, stale, err := s.Read("work/a.md")
	require.NoError(t, err)
	_, err = s.Write("work/a.md", []byte("two"), stale)
	require.NoError(t, err)

	_, err = s.Write("work/a.md", []byte("three"), stale)
	require.True(t, errors.Is(err, store.ErrVersionConflict), "want ErrVersionConflict, got %v", err)
}

func TestWriteWithEmptyVersionSkipsTheCheck(t *testing.T) {
	s := newStore(t)
	_, err := s.Write("work/new.md", []byte("fresh"), "")
	require.NoError(t, err)
}

func TestListFiltersByPrefix(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: "work/a.md", Data: []byte("a")},
		{Path: "personal/b.md", Data: []byte("b")},
	}, "Dhia <d@x>", "two scopes")
	require.NoError(t, err)

	paths, err := s.List("work/")
	require.NoError(t, err)
	require.Equal(t, []string{"work/a.md"}, paths)
}

func TestAuthorIsRecordedOnTheCommit(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("a")}},
		"agent:compile <a@x>", "compiled")
	require.NoError(t, err)

	history, err := s.History("work/a.md", 1)
	require.NoError(t, err)
	require.Contains(t, history[0].Author, "agent:compile")
}

func TestReadMissingFileErrors(t *testing.T) {
	_, _, err := newStore(t).Read("work/nope.md")
	require.Error(t, err)
}

// TestCommitAppliesAStagedDeletion covers store.Change{Delete: true}: nothing in this system
// could remove a page before it existed, so a page that moved — a corpus reclassified, a
// tenant corrected — left its previous file on disk forever, and the indexer kept indexing it.
func TestCommitAppliesAStagedDeletion(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("a")}}, "Dhia <d@x>", "add a")
	require.NoError(t, err)

	_, err = s.Commit([]store.Change{
		{Path: "client-globex/a.md", Data: []byte("a")},
		{Path: "work/a.md", Delete: true},
	}, "Dhia <d@x>", "move a into client-globex")
	require.NoError(t, err)

	paths, err := s.List("")
	require.NoError(t, err)
	require.Equal(t, []string{"client-globex/a.md"}, paths)

	_, _, err = s.Read("work/a.md")
	require.Error(t, err, "the superseded path must be gone from the working tree too")
	require.Equal(t, []string{"client-globex/a.md"}, headPaths(t, s),
		"and out of HEAD, not merely off disk")
}

// TestCommitWithNothingStagedNextToAnUntrackedFile: Status.IsClean() reports false for an
// untracked file, so a single stray file in the vault slips past the clean-tree guard while
// the staged changes are still empty, and go-git refuses the commit. "Nothing changed" is a
// success for every caller here, so ErrEmptyCommit has to be caught too.
func TestCommitWithNothingStagedNextToAnUntrackedFile(t *testing.T) {
	s := newStore(t)
	first, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("a")}},
		"Dhia <d@x>", "add a")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(s.Root(), "stray.txt"), []byte("x"), 0o644))

	again, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("a")}},
		"Dhia <d@x>", "rewrite a with identical bytes")
	require.NoError(t, err)
	require.Equal(t, first, again, "an effectively empty commit reports the commit already at HEAD")
}

// TestDeletingAnAbsentPathIsNotAnError: staging a removal states an end state, so a caller
// superseding a page need not first check whether an earlier run already removed it.
func TestDeletingAnAbsentPathIsNotAnError(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{
		{Path: "work/a.md", Data: []byte("a")},
		{Path: "work/never-existed.md", Delete: true},
	}, "Dhia <d@x>", "add one, remove nothing")
	require.NoError(t, err)
}

// TestDeletingAnUncommittedFileStillRemovesIt: pages are edited outside this process, so a
// file can be on disk without ever having been staged. worktree.Remove has no index entry to
// work from there; leaving the file behind would mean List and reindex kept seeing a page the
// caller asked to remove.
func TestDeletingAnUncommittedFileStillRemovesIt(t *testing.T) {
	s := newStore(t)
	_, err := s.Commit([]store.Change{{Path: "work/a.md", Data: []byte("a")}}, "Dhia <d@x>", "add a")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(s.Root(), "work"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s.Root(), "work/dropped.md"), []byte("x"), 0o644))

	_, err = s.Commit([]store.Change{{Path: "work/dropped.md", Delete: true}}, "Dhia <d@x>", "drop")
	require.NoError(t, err)

	paths, err := s.List("")
	require.NoError(t, err)
	require.Equal(t, []string{"work/a.md"}, paths)
}
