package vault_test

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/dhia/balise/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestNewUIDIs26CharULID(t *testing.T) {
	uid := vault.NewUID()
	require.Len(t, uid, 26)
	require.Equal(t, uid, strings.ToUpper(uid))
}

func TestUIDsAreUniqueAndSortByCreation(t *testing.T) {
	uids := make([]string, 100)
	for i := range uids {
		uids[i] = vault.NewUID()
	}
	seen := map[string]bool{}
	for _, u := range uids {
		require.False(t, seen[u], "duplicate ULID")
		seen[u] = true
	}
	sorted := append([]string(nil), uids...)
	sort.Strings(sorted)
	require.Equal(t, sorted, uids, "ULIDs must sort by creation time")
}

func TestNewUIDIsSafeForConcurrentUse(t *testing.T) {
	const n = 200
	results := make(chan string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			results <- vault.NewUID()
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[string]bool, n)
	for uid := range results {
		require.False(t, seen[uid], "duplicate ULID from concurrent NewUID calls: %s", uid)
		seen[uid] = true
	}
}

func TestSlugFromFilename(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"project_aks_pod_cidr.md", "aks-pod-cidr"},
		{"feedback_minimal_comments.md", "minimal-comments"},
		{"reference_gh_account_for_repo.md", "gh-account-for-repo"},
		{"project-globex-onboarding.md", "globex-onboarding"},
		{"globex-overview.md", "globex-overview"},
		{"projections_quarterly.md", "projections-quarterly"},
		{"MEMORY.md", "memory"},
	} {
		require.Equal(t, tc.want, vault.SlugFromFilename(tc.in), tc.in)
	}
}

func TestNormaliseFoldsSnakeAndCase(t *testing.T) {
	require.Equal(t, "aks-pod-cidr", vault.Normalise("Project_AKS_Pod_CIDR.md"))
	require.Equal(t, "aks-pod-cidr", vault.Normalise("aks-pod-cidr"))
	require.Equal(t, "aks-pod-cidr", vault.Normalise("  aks_pod_cidr  "))
}

// TestNormaliseTreatsHyphenatedAndUnderscoredPrefixesTheSame is a regression test for a
// real bug found running Task 15's acceptance suite against the real corpus: a wikilink
// target spelled with a hyphenated prefix, `[[project-globex-onboarding]]`, failed to
// normalise to the same slug as the target page's own underscore-joined alias
// `project_globex_onboarding`, because typePrefixes only recognised the underscore spelling.
// The ref then normalised to "project-globex-onboarding" while the page's own alias
// normalised to "globex-onboarding" — two different strings for what should be one
// reference — so the normalised rung of the resolution ladder silently failed to unify
// them and the link read as dangling.
func TestNormaliseTreatsHyphenatedAndUnderscoredPrefixesTheSame(t *testing.T) {
	require.Equal(t, "globex-onboarding", vault.Normalise("project-globex-onboarding"))
	require.Equal(t, "globex-onboarding", vault.Normalise("project_globex_onboarding"))
}
