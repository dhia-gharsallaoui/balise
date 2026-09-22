package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// newDefaultsTestCmd builds a cobra.Command with a --defaults flag registered exactly
// the way every real subcommand registers it (StringVar, default "defaults"), so these
// tests exercise cmd.Flags().Changed the same way resolveDefaultsDir's real callers do.
func newDefaultsTestCmd() (*cobra.Command, *string) {
	var defaults string
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&defaults, "defaults", "defaults", defaultsFlagHelp)
	return cmd, &defaults
}

// TestResolveDefaultsDir_FallsBackToEmbedded_FromArbitraryCwd is the regression test for
// the bug this fix addresses: `balise mcp --stdio` (and every other subcommand sharing
// --defaults) failed whenever it was spawned from a working directory with no defaults/
// directory anywhere near it -- exactly how an MCP host (Claude Code, Claude Desktop,
// etc.) spawns a stdio server: no repo checkout, no cwd assumption at all. Before this
// fix, the unresolved "defaults" flag value was joined straight onto a relative path and
// read via os.ReadFile/filepath.Glob, which failed the instant the process's cwd held no
// defaults/ directory.
//
// t.Chdir moves the process's cwd to a directory that is neither this repo nor anywhere
// containing a defaults/ directory (and the vault itself is deliberately given none
// either), so a regression back to a bare cwd-relative lookup, or any break in the
// vault/embedded fallback chain, fails this test exactly the way real users hit the
// original bug.
func TestResolveDefaultsDir_FallsBackToEmbedded_FromArbitraryCwd(t *testing.T) {
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatalf("create vault dir: %v", err)
	}

	cmd, defaults := newDefaultsTestCmd()

	dir, cleanup, err := resolveDefaultsDir(cmd, "defaults", *defaults, vault)
	if err != nil {
		t.Fatalf("resolveDefaultsDir: %v", err)
	}
	defer cleanup()

	for _, want := range []string{"order.yaml", "scopes.yaml", "types", "facets"} {
		if _, statErr := os.Stat(filepath.Join(dir, want)); statErr != nil {
			t.Errorf("embedded defaults missing %s: %v", want, statErr)
		}
	}

	// The embedded fallback must never carry the owner's real, gitignored files --
	// only their tracked .example siblings -- so a compiled binary never ships real
	// customer/tenant data to whoever runs it.
	for _, mustNotExist := range []string{"tenants.yaml", "spaces.yaml", filepath.Join("facets", "customer.yaml")} {
		if _, statErr := os.Stat(filepath.Join(dir, mustNotExist)); statErr == nil {
			t.Errorf("embedded defaults leaked real file %s", mustNotExist)
		}
	}
	for _, mustExist := range []string{"tenants.example.yaml", "spaces.example.yaml", filepath.Join("facets", "customer.example.yaml")} {
		if _, statErr := os.Stat(filepath.Join(dir, mustExist)); statErr != nil {
			t.Errorf("embedded defaults missing generic fallback %s: %v", mustExist, statErr)
		}
	}
}

// TestResolveDefaultsDir_ExplicitFlagAlwaysWins verifies tier 1 of the resolution order:
// an explicit --defaults beats even a vault that carries its own defaults/ directory.
func TestResolveDefaultsDir_ExplicitFlagAlwaysWins(t *testing.T) {
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(vault, "defaults"), 0o755); err != nil {
		t.Fatalf("create vault/defaults dir: %v", err)
	}

	explicit := filepath.Join(t.TempDir(), "custom-defaults")
	if err := os.MkdirAll(explicit, 0o755); err != nil {
		t.Fatalf("create explicit defaults dir: %v", err)
	}

	cmd, defaults := newDefaultsTestCmd()
	if err := cmd.Flags().Set("defaults", explicit); err != nil {
		t.Fatalf("set --defaults: %v", err)
	}

	dir, cleanup, err := resolveDefaultsDir(cmd, "defaults", *defaults, vault)
	if err != nil {
		t.Fatalf("resolveDefaultsDir: %v", err)
	}
	defer cleanup()

	if dir != explicit {
		t.Errorf("explicit --defaults did not win: got %s, want %s", dir, explicit)
	}
}

// TestResolveDefaultsDir_ExplicitFlagMissingIsHardError verifies that naming a path that
// does not exist is a clear error, never a silent fall-through to the embedded copy.
func TestResolveDefaultsDir_ExplicitFlagMissingIsHardError(t *testing.T) {
	vault := t.TempDir()
	cmd, defaults := newDefaultsTestCmd()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := cmd.Flags().Set("defaults", missing); err != nil {
		t.Fatalf("set --defaults: %v", err)
	}

	_, cleanup, err := resolveDefaultsDir(cmd, "defaults", *defaults, vault)
	defer cleanup()
	if err == nil {
		t.Fatal("expected an error for an explicit --defaults pointing at a missing directory, got nil")
	}
}

// TestResolveDefaultsDir_PrefersVaultInsideOverBeside verifies tier 2 beats tier 3: a
// vault carrying its own defaults/ wins over one that merely sits beside a same-named
// directory.
func TestResolveDefaultsDir_PrefersVaultInsideOverBeside(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	if err := os.MkdirAll(filepath.Join(vault, "defaults"), 0o755); err != nil {
		t.Fatalf("create vault/defaults: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "defaults"), 0o755); err != nil {
		t.Fatalf("create sibling defaults: %v", err)
	}

	cmd, defaults := newDefaultsTestCmd()
	dir, cleanup, err := resolveDefaultsDir(cmd, "defaults", *defaults, vault)
	if err != nil {
		t.Fatalf("resolveDefaultsDir: %v", err)
	}
	defer cleanup()

	want := filepath.Join(vault, "defaults")
	if dir != want {
		t.Errorf("got %s, want %s (vault-inside should win over vault-sibling)", dir, want)
	}
}

// TestResolveDefaultsDir_FallsBackToSiblingWhenNotInsideVault verifies tier 3: a
// defaults/ directory beside the vault (in its parent) is used when the vault carries
// none of its own.
func TestResolveDefaultsDir_FallsBackToSiblingWhenNotInsideVault(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatalf("create vault: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "defaults"), 0o755); err != nil {
		t.Fatalf("create sibling defaults: %v", err)
	}

	cmd, defaults := newDefaultsTestCmd()
	dir, cleanup, err := resolveDefaultsDir(cmd, "defaults", *defaults, vault)
	if err != nil {
		t.Fatalf("resolveDefaultsDir: %v", err)
	}
	defer cleanup()

	want := filepath.Join(parent, "defaults")
	if dir != want {
		t.Errorf("got %s, want %s", dir, want)
	}
}

// TestResolveDefaultsDir_ExplicitRelativePathResolvesAgainstCwd guards `make demo`, which
// passes --defaults demo-vault-defaults (a relative path) while running from the repo
// root. The resolved directory must stay exactly as given -- still relative -- so every
// caller that joins further path segments onto it keeps resolving against the same cwd
// make already runs from.
func TestResolveDefaultsDir_ExplicitRelativePathResolvesAgainstCwd(t *testing.T) {
	repoLike := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoLike, "demo-vault-defaults"), 0o755); err != nil {
		t.Fatalf("create demo-vault-defaults: %v", err)
	}
	t.Chdir(repoLike)

	vault := t.TempDir()
	cmd, defaults := newDefaultsTestCmd()
	if err := cmd.Flags().Set("defaults", "demo-vault-defaults"); err != nil {
		t.Fatalf("set --defaults: %v", err)
	}

	dir, cleanup, err := resolveDefaultsDir(cmd, "defaults", *defaults, vault)
	if err != nil {
		t.Fatalf("resolveDefaultsDir: %v (this is exactly what `make demo`'s --defaults demo-vault-defaults relies on)", err)
	}
	defer cleanup()

	if dir != "demo-vault-defaults" {
		t.Errorf("got %s, want the relative path unchanged (demo-vault-defaults)", dir)
	}
}
