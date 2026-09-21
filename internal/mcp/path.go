// Package mcp implements balise's agent surface: an MCP server, mounted at
// /mcp alongside the REST API, exposing remember (write), search, and fetch
// (read) to holders of an agent_tokens row. See 04-technical-spec-v1.md
// section 12 (MCP server) and section 14 (scope and security model).
package mcp

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dhia/balise/internal/vault"
)

// ErrInvalidSegment is an alias for vault.ErrInvalidSegment: the segment validator this
// package relied on moved to internal/vault so internal/store's CreateToken could share it
// without internal/store importing internal/mcp (mcp already depends on store; the reverse
// would invert that relationship). The alias exists so anything in this package -- and any
// errors.Is check against mcp.ErrInvalidSegment -- keeps working unchanged. See
// internal/vault/segment.go for the full rationale.
var ErrInvalidSegment = vault.ErrInvalidSegment

// validateSegment is a thin wrapper over vault.ValidateSegment, kept under this package's
// original unexported name so this file's other callers, and path_test.go, need no change.
// The validation logic itself lives in exactly one place: internal/vault.ValidateSegment.
// Do not reimplement it here -- a second copy would drift from the shared one, silently.
func validateSegment(s string) error {
	return vault.ValidateSegment(s)
}

// rememberPath builds the vault-relative path remember appends to, for the
// given scope and agent name, on the given date (used only for the
// per-day file name). Both scope and agent come from outside this
// process -- scope from a tool argument, agent from the calling token's
// name -- so neither is interpolated into a path without validation first.
//
// The result always has the shape "<scope>/memory/<agent>/YYYY-MM-DD.md".
// As a second, independent check (belt-and-braces, per the task brief),
// the candidate path is run through path.Clean and the cleaned result is
// verified to still start with the literal "<scope>/memory/<agent>/"
// prefix before it is returned -- so even a latent bug in validateSegment
// could not, by itself, produce a path that escapes that prefix.
func rememberPath(scope, agent string, when time.Time) (string, error) {
	if err := validateSegment(scope); err != nil {
		return "", fmt.Errorf("scope: %w", err)
	}
	if err := validateSegment(agent); err != nil {
		return "", fmt.Errorf("agent: %w", err)
	}

	prefix := scope + "/memory/" + agent + "/"
	candidate := prefix + when.UTC().Format("2006-01-02") + ".md"

	cleaned := path.Clean(candidate)
	if cleaned != candidate || !strings.HasPrefix(cleaned, prefix) {
		return "", fmt.Errorf("%w: %q escapes expected prefix %q", ErrInvalidSegment, candidate, prefix)
	}

	return cleaned, nil
}
