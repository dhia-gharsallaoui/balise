package compile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dhia/balise/internal/store"
)

// statePath is where extract_claims records, per page, the body_hash it last ran
// against successfully. A page whose body_hash hasn't changed since is skipped on
// the next run — this is the cost control from 04 section 9.3 ("skip a page whose
// body hasn't changed"), not an acceptance record: it only prevents redundant LLM
// calls and carries no opinion about whether a prior proposal was accepted.
const statePath = ".balise/compile/extract-claims-state.json"

// PageState is one page's last successful extraction.
type PageState struct {
	BodyHash string `json:"body_hash"`
	RanAt    string `json:"ran_at"`
}

// State is the whole extract_claims run-state file. It is immutable by
// convention: every mutating method returns a new State rather than editing the
// receiver in place, so a caller can hold onto an old State (e.g. to diff what
// changed) without it shifting under them.
type State struct {
	Pages map[string]PageState `json:"pages"`
}

// LoadState reads statePath from pages. A missing file is not an error — it is
// the normal case on a vault's first extract_claims run — and yields an empty
// State. GitPageStore.Read wraps the underlying os.ErrNotExist with %w, so
// errors.Is still unwraps it correctly here.
func LoadState(pages store.PageStore) (State, error) {
	data, _, err := pages.Read(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{Pages: map[string]PageState{}}, nil
		}
		return State{}, fmt.Errorf("read %s: %w", statePath, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", statePath, err)
	}
	if s.Pages == nil {
		s.Pages = map[string]PageState{}
	}
	return s, nil
}

// Unchanged reports whether path was last extracted against exactly this
// bodyHash, meaning extract_claims has nothing new to say about it.
func (s State) Unchanged(path, bodyHash string) bool {
	prior, ok := s.Pages[path]
	return ok && prior.BodyHash == bodyHash
}

// WithRecorded returns a new State with path's extraction recorded at bodyHash
// and at, leaving the receiver untouched.
func (s State) WithRecorded(path, bodyHash string, at time.Time) State {
	next := make(map[string]PageState, len(s.Pages)+1)
	for k, v := range s.Pages {
		next[k] = v
	}
	next[path] = PageState{BodyHash: bodyHash, RanAt: at.UTC().Format(time.RFC3339)}
	return State{Pages: next}
}

// Render serializes the state as indented JSON for writing back through PageStore.
func (s State) Render() ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal extract_claims state: %w", err)
	}
	return append(data, '\n'), nil
}
