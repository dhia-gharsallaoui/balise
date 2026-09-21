package vault_test

import (
	"testing"

	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

// TestValidateSegmentRejectsHostileInput mirrors internal/mcp's original
// path_test.go coverage for the validator that moved here, plus the
// whitespace-only case internal/store.CreateToken also needs rejected at
// mint time.
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
		{"bare unicode line separator", " "},
		{"embedded newline", "agent\nname"},
		{"embedded carriage return", "agent\rname"},
		{"embedded tab", "agent\tname"},
		{"whitespace only: single space", " "},
		{"whitespace only: several spaces", "   "},
		{"whitespace only: tabs", "\t\t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := vault.ValidateSegment(tc.in)
			require.Error(t, err, "ValidateSegment(%q) must be rejected", tc.in)
			require.ErrorIs(t, err, vault.ErrInvalidSegment)
		})
	}
}

func TestValidateSegmentAcceptsOrdinaryNames(t *testing.T) {
	for _, ok := range []string{"work", "claude-code", "client-globex", "agent_42", "a", "good-agent"} {
		require.NoError(t, vault.ValidateSegment(ok), "ValidateSegment(%q) should be accepted", ok)
	}
}
