package registry

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dhia/balise/internal/vault"
)

// DeclaredScopes is the owner-editable list of scope names declared in defaults/scopes.yaml
// — scopes that exist even though the vault holds no page under them yet. A scope normally
// comes into being the moment a page under it is written (see store.DiscoverScopes and
// cmd/balise's vault scan); that makes an empty scope impossible to create ahead of time.
// This file is the other half: the effective scope set a running server enforces is always
// declared ∪ discovered (see internal/store/scopes_discovery.go's EffectiveScopes).
//
// Unlike Tenants and Order (LoadTenants, LoadOrder in this package), an empty declared list
// is the normal starting state, not an error — defaults/scopes.yaml ships with `scopes: []`
// and stays that way until the owner adds one.
type DeclaredScopes struct{ names []string }

// LoadDeclaredScopes reads defaults/scopes.yaml. A missing or unparsable file is an error
// (the file is expected to exist, shipped by this repo); an empty `scopes:` list is not.
func LoadDeclaredScopes(path string) (*DeclaredScopes, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	names, err := parseDeclaredScopes(body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &DeclaredScopes{names: names}, nil
}

func parseDeclaredScopes(body []byte) ([]string, error) {
	var raw struct {
		Scopes []string `yaml:"scopes"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return raw.Scopes, nil
}

// Names returns the declared scopes, in file order. The returned slice is a copy: callers
// may not mutate DeclaredScopes through it.
func (d *DeclaredScopes) Names() []string { return append([]string(nil), d.names...) }

// Has reports whether name is already declared.
func (d *DeclaredScopes) Has(name string) bool {
	for _, n := range d.names {
		if n == name {
			return true
		}
	}
	return false
}

// ErrScopeAlreadyDeclared means AppendDeclaredScope was asked to add a name already present
// in defaults/scopes.yaml.
var ErrScopeAlreadyDeclared = errors.New("scope already declared")

// AppendDeclaredScope adds name to defaults/scopes.yaml on disk and returns the full,
// updated declared list. It is add-only by construction — there is no corresponding rename
// or remove function anywhere in this codebase, and none should be added: renaming would
// orphan every page already filed under the old name and silently change what an existing
// agent token's scope list refers to, and deleting is destructive in the same way. See the
// file's own header comment.
//
// name is validated with vault.ValidateSegment — the same validator token minting (and the
// MCP path) already use, not a second copy of that check — before anything touches disk.
// Callers are responsible for serializing concurrent calls (internal/api holds a mutex
// around this, since it also rebuilds the server's live *store.Queries from the result).
func AppendDeclaredScope(path, name string) (*DeclaredScopes, error) {
	if err := vault.ValidateSegment(name); err != nil {
		return nil, fmt.Errorf("scope name: %w", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	current, err := parseDeclaredScopes(body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, existing := range current {
		if existing == name {
			return nil, fmt.Errorf("%s: %w", name, ErrScopeAlreadyDeclared)
		}
	}
	updated := append(append([]string(nil), current...), name)

	lines := strings.Split(string(body), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, "scopes:") {
			lines[i] = "scopes: [" + quotedScopeList(updated) + "]"
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: no scopes: line found", path)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return &DeclaredScopes{names: updated}, nil
}

// quotedScopeList renders names as a YAML flow-style sequence of double-quoted scalars.
// Quoting every element (rather than emitting bare words, as defaults/tenants.yaml does)
// means a scope name can never be misparsed as a YAML flow-sequence control character even
// though ValidateSegment's rejection list does not itself forbid every such character.
func quotedScopeList(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = strconv.Quote(n)
	}
	return strings.Join(parts, ", ")
}
