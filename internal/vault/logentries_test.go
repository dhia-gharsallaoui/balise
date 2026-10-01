package vault_test

import (
	"testing"

	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestLogEntriesSplitsTimestampsAndJoinsContinuationLines(t *testing.T) {
	body := "preamble that is not an entry\n" +
		"- **06:31:09 UTC** first note\n" +
		"- **07:10:00 UTC** second note,\n  continued here\n\n" +
		"- **08:00:00 UTC**   \n"
	require.Equal(t, []vault.LogEntry{
		{Time: "06:31:09", Text: "first note"},
		{Time: "07:10:00", Text: "second note, continued here"},
	}, vault.LogEntries(body))
}
