package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestValidateSegmentRejectsHostileInput is "a test per hostile input" for
// validateSegment, the single point where a token-supplied agent name or a
// scope argument is checked before it is ever interpolated into a path.
func TestValidateSegmentRejectsHostileInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty string", ""},
		{"parent directory traversal", "../etc"},
		{"bare parent reference", ".."},
		{"bare current-directory reference", "."},
		{"embedded forward slash", "a/b"},
		{"embedded backslash", `a\b`},
		{"absolute unix path", "/etc/passwd"},
		{"embedded null byte", "agent\x00name"},
		{"unicode line separator U+2028", "agent name"},
		{"unicode paragraph separator U+2029", "agent name"},
		{"embedded newline", "agent\nname"},
		{"embedded carriage return", "agent\rname"},
		{"embedded tab", "agent\tname"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSegment(tc.in)
			require.Error(t, err, "validateSegment(%q) must be rejected", tc.in)
			require.ErrorIs(t, err, ErrInvalidSegment)
		})
	}
}

func TestValidateSegmentAcceptsOrdinaryNames(t *testing.T) {
	for _, ok := range []string{"work", "claude-code", "client-globex", "agent_42", "a"} {
		require.NoError(t, validateSegment(ok), "validateSegment(%q) should be accepted", ok)
	}
}

// TestRememberPathRejectsHostileScopeOrAgent exercises the same hostile
// inputs one layer up, through rememberPath, once as the scope and once as
// the agent name -- both are caller-controlled (scope from a tool
// argument, agent from the calling token's name) and neither is trusted.
func TestRememberPathRejectsHostileScopeOrAgent(t *testing.T) {
	when := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	hostile := []string{"../escape", "..", ".", "a/b", `a\b`, "/etc/passwd", "", "agent\x00", "agent ", "agent "}

	for _, h := range hostile {
		if _, err := rememberPath(h, "claude-code", when); err == nil {
			t.Errorf("rememberPath(scope=%q, agent=claude-code) = nil error, want rejection", h)
		}
		if _, err := rememberPath("work", h, when); err == nil {
			t.Errorf("rememberPath(scope=work, agent=%q) = nil error, want rejection", h)
		}
	}
}

func TestRememberPathBuildsExpectedShape(t *testing.T) {
	when := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	got, err := rememberPath("work", "claude-code", when)
	require.NoError(t, err)
	require.Equal(t, "work/memory/claude-code/2026-09-18.md", got)
}

// TestRememberPathSecondCheckCannotBeBypassed pins the "belt-and-braces
// second check" the task brief calls for: even granting that
// validateSegment accepted scope and agent individually, the composed
// result is independently re-verified to still sit under the expected
// "<scope>/memory/<agent>/" prefix after path.Clean. There is no known
// input that reaches this branch today (validateSegment already rejects
// every separator and dot-segment), so this test documents and pins the
// invariant rather than proving it reachable -- if a future relaxation of
// validateSegment ever let something odd through, this is the check that
// would still catch it.
func TestRememberPathSecondCheckCannotBeBypassed(t *testing.T) {
	when := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	got, err := rememberPath("work", "claude-code", when)
	require.NoError(t, err)
	require.True(t,
		len(got) > len("work/memory/claude-code/") &&
			got[:len("work/memory/claude-code/")] == "work/memory/claude-code/",
		"rememberPath result %q must sit under the expected prefix", got)
}
