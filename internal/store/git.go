package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ErrVersionConflict means the file changed since the caller last read it.
var ErrVersionConflict = errors.New("version conflict")

// GitPageStore is the only type that touches the repository. It uses go-git rather than
// shelling out, so the binary needs no git on PATH — spec section 3.1's single-binary goal.
type GitPageStore struct {
	root string
	repo *git.Repository
}

var _ PageStore = (*GitPageStore)(nil)

// InitGit creates a repository at root.
func InitGit(root string) (*GitPageStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}
	repo, err := git.PlainInit(root, false)
	if errors.Is(err, git.ErrRepositoryAlreadyExists) {
		return OpenGit(root)
	}
	if err != nil {
		return nil, fmt.Errorf("git init %s: %w", root, err)
	}
	return &GitPageStore{root: root, repo: repo}, nil
}

// OpenGit opens an existing repository at root.
func OpenGit(root string) (*GitPageStore, error) {
	repo, err := git.PlainOpen(root)
	if err != nil {
		return nil, fmt.Errorf("git open %s: %w", root, err)
	}
	return &GitPageStore{root: root, repo: repo}, nil
}

// Root returns the working tree path.
func (s *GitPageStore) Root() string { return s.root }

// Read returns the file's bytes and its blob SHA.
func (s *GitPageStore) Read(path string) ([]byte, string, error) {
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(path)))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	return data, blobHash(data), nil
}

// Write replaces one file, refusing when ifVersion no longer matches. An empty
// ifVersion skips the check, which is what a first write of a new file wants.
func (s *GitPageStore) Write(path string, data []byte, ifVersion string) (string, error) {
	if ifVersion != "" {
		current, _, err := s.Read(path)
		if err == nil && blobHash(current) != ifVersion {
			return "", fmt.Errorf("%s: %w", path, ErrVersionConflict)
		}
	}
	return s.Commit([]Change{{Path: path, Data: data}},
		"balise <balise@localhost>", "update "+path)
}

// List returns repo-relative, slash-separated paths under prefix, sorted. It walks the
// working tree directly rather than reading HEAD's tree: pages are edited outside this
// process (Obsidian, vim) and a `reindex` that calls List then Read must see exactly
// what is on disk, including files nobody has committed yet — so List and Read share the
// same source of truth (the filesystem), not the git object database.
func (s *GitPageStore) List(prefix string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(s.root, func(abs string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(s.root, abs)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(rel)
		if strings.HasPrefix(slashed, prefix) {
			paths = append(paths, slashed)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", s.root, err)
	}
	sort.Strings(paths)
	return paths, nil
}

// Commit writes every change and records them as one commit.
func (s *GitPageStore) Commit(changes []Change, author, message string) (string, error) {
	worktree, err := s.repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}
	for _, change := range changes {
		if change.Delete {
			if err := s.stageRemoval(worktree, change.Path); err != nil {
				return "", err
			}
			continue
		}
		abs := filepath.Join(s.root, filepath.FromSlash(change.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", fmt.Errorf("mkdir for %s: %w", change.Path, err)
		}
		if err := os.WriteFile(abs, change.Data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", change.Path, err)
		}
		if _, err := worktree.Add(change.Path); err != nil {
			return "", fmt.Errorf("stage %s: %w", change.Path, err)
		}
	}
	// Staging can leave nothing to commit — a re-import that rewrote every page with the
	// identical bytes it already held, or a removal of a path that was already gone. go-git
	// refuses an empty commit, and rightly, but "this changed nothing" is a success for
	// every caller here: reporting the commit the vault is already at keeps a repeated
	// import idempotent instead of turning a no-op into a failure.
	//
	// The check is made twice because neither alone is enough. Status.IsClean() reports false
	// for an *untracked* file, so a single stray file anywhere in the vault — a page dropped
	// in by hand, an editor's leftover — would slip past it while the staged changes were
	// still empty; catching git.ErrEmptyCommit afterwards covers that. The IsClean() check is
	// kept in front of it because it answers the question without building a commit object at
	// all. A clean tree with no HEAD (an empty repository) falls through both, so go-git's own
	// error surfaces rather than a fabricated SHA.
	status, err := worktree.Status()
	if err != nil {
		return "", fmt.Errorf("status: %w", err)
	}
	if status.IsClean() {
		if head, headErr := s.repo.Head(); headErr == nil {
			return head.Hash().String(), nil
		}
	}

	name, email := splitAuthor(author)
	hash, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: name, Email: email, When: time.Now()},
	})
	if errors.Is(err, git.ErrEmptyCommit) {
		if head, headErr := s.repo.Head(); headErr == nil {
			return head.Hash().String(), nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return hash.String(), nil
}

// stageRemoval deletes path from the working tree and stages the deletion. A path that is
// already absent is not an error: staging a removal is a statement about the end state, and a
// caller that supersedes a page should not have to first check whether some earlier run
// already removed it.
//
// A tracked path goes through worktree.Remove, and its error is returned. It used to be
// discarded whenever a direct os.Remove then succeeded, which is the worst of both outcomes:
// the file leaves disk but stays in HEAD, so the vault's system of record still claims a page
// that no longer exists and the next checkout resurrects it. That is data loss dressed up as
// success.
//
// An untracked path (hand-dropped into the vault, never committed) has no index entry for
// worktree.Remove to work from and nothing in HEAD to stage a deletion against, so deleting it
// from disk is the whole operation — leaving it there would mean List and reindex kept seeing
// a file the caller asked to remove.
func (s *GitPageStore) stageRemoval(worktree *git.Worktree, path string) error {
	abs := filepath.Join(s.root, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	tracked, err := s.isTracked(path)
	if err != nil {
		return err
	}
	if !tracked {
		if err := os.Remove(abs); err != nil {
			return fmt.Errorf("remove untracked %s: %w", path, err)
		}
		return nil
	}
	if _, err := worktree.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// isTracked reports whether path has an index entry — i.e. whether git knows about it at all.
func (s *GitPageStore) isTracked(path string) (bool, error) {
	idx, err := s.repo.Storer.Index()
	if err != nil {
		return false, fmt.Errorf("read index: %w", err)
	}
	if _, err := idx.Entry(path); err != nil {
		if errors.Is(err, index.ErrEntryNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("index entry %s: %w", path, err)
	}
	return true, nil
}

// History returns up to limit commits touching path, newest first.
func (s *GitPageStore) History(path string, limit int) ([]Commit, error) {
	iter, err := s.repo.Log(&git.LogOptions{FileName: &path})
	if err != nil {
		return nil, fmt.Errorf("log %s: %w", path, err)
	}
	defer iter.Close()

	var out []Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if len(out) >= limit {
			return storeStopIteration
		}
		out = append(out, Commit{
			SHA:     c.Hash.String(),
			Author:  fmt.Sprintf("%s <%s>", c.Author.Name, c.Author.Email),
			When:    c.Author.When,
			Message: strings.TrimSpace(c.Message),
		})
		return nil
	})
	if err != nil && !errors.Is(err, storeStopIteration) {
		return nil, fmt.Errorf("walk log %s: %w", path, err)
	}
	return out, nil
}

var storeStopIteration = errors.New("stop")

// blobHash computes git's blob SHA without writing an object.
func blobHash(data []byte) string {
	return plumbing.ComputeHash(plumbing.BlobObject, data).String()
}

// splitAuthor parses "Name <email>" into its parts. If there is no "<...>"
// segment, the whole string is treated as the name and email is left empty.
func splitAuthor(author string) (name, email string) {
	open := strings.Index(author, "<")
	closeAt := strings.LastIndex(author, ">")
	if open == -1 || closeAt == -1 || closeAt < open {
		return strings.TrimSpace(author), ""
	}
	return strings.TrimSpace(author[:open]), author[open+1 : closeAt]
}
