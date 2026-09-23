package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	balise "github.com/dhia/balise"
	"github.com/spf13/cobra"
)

// defaultsFlagHelp documents the --defaults resolution order once, so every subcommand
// that takes the flag (mcp, reindex, serve, import, compile extract-claims) shows the
// same explanation in --help. See resolveDefaultsDir for the implementation and the
// reasoning behind the order.
const defaultsFlagHelp = "directory holding types/, facets/, spaces.yaml, order.yaml, tenants.yaml and scopes.yaml " +
	"(resolution when not given: <vault>/defaults, then defaults/ next to <vault>, then the binary's embedded copy)"

// resolveDefaultsDir implements the --defaults resolution order shared by every
// subcommand that takes the flag:
//
//  1. An explicit --defaults always wins. cmd.Flags().Changed is used rather than a
//     string comparison against the flag's zero-value default ("defaults"), because
//     someone who types `--defaults defaults` on purpose is indistinguishable from the
//     unset case by value alone. A path that does not exist here is a hard error —
//     someone who named a path wants that path, not a silent fall-through to the
//     embedded copy.
//  2. <vault>/defaults — checked first among the two auto-detected locations, since a
//     vault carrying its own registry inside itself is a more specific, more deliberate
//     signal than a same-named directory that merely happens to sit beside it.
//  3. defaults/ next to the vault (i.e. inside the vault's parent directory) — so one
//     registry can be shared by sibling vaults without living inside any single one of
//     them.
//  4. The embedded copy (see the root package's EmbeddedDefaults, embedded from the
//     tracked, generic subset of defaults/), extracted to a fresh temp directory. This
//     is the final fallback and always succeeds, which is what makes
//     `balise mcp --stdio <vault>` work with exactly the README's documented MCP
//     config: no --defaults flag, and no assumption about the spawning process's
//     working directory — an MCP host does not run the command from this repo, or from
//     any particular directory at all.
//
// The returned cleanup func removes the temp directory created for case 4; it is a
// no-op for the other three. Callers should always `defer cleanup()`.
func resolveDefaultsDir(cmd *cobra.Command, flagName, flagValue, vault string) (dir string, cleanup func(), err error) {
	noop := func() {}

	// Every successful return announces which registry it picked, on stderr.
	//
	// Stderr, not stdout, because `mcp --stdio` speaks JSON-RPC on stdout and a stray
	// line there corrupts the stream for the host.
	//
	// It is printed at all because the resolution order deliberately does not consult
	// the working directory, which is what makes the documented MCP config work from
	// anywhere -- and which also means someone standing in this repo editing defaults/
	// and running `balise reindex <vault>` gets the embedded copy and no hint that
	// their edits were ignored. That failure is silent and looks exactly like a bug in
	// whatever they were changing. One line makes it visible instead.
	announce := func(chosen, source string) {
		fmt.Fprintf(cmd.ErrOrStderr(), "defaults: %s (%s)\n", chosen, source)
	}

	if cmd.Flags().Changed(flagName) {
		info, statErr := os.Stat(flagValue)
		if statErr != nil {
			return "", noop, fmt.Errorf("--%s %s: %w", flagName, flagValue, statErr)
		}
		if !info.IsDir() {
			return "", noop, fmt.Errorf("--%s %s: not a directory", flagName, flagValue)
		}
		announce(flagValue, "--"+flagName)
		return flagValue, noop, nil
	}

	if inside := filepath.Join(vault, "defaults"); isDir(inside) {
		announce(inside, "inside the vault")
		return inside, noop, nil
	}
	if beside := filepath.Join(filepath.Dir(filepath.Clean(vault)), "defaults"); isDir(beside) {
		announce(beside, "beside the vault")
		return beside, noop, nil
	}

	// Named rather than shown as its temp path: the extracted directory is an internal
	// detail that changes every run, and "embedded" is the fact that matters -- it is
	// what tells a reader their own defaults/ was not used.
	embedded, cleanupEmbedded, embedErr := extractEmbeddedDefaults()
	if embedErr == nil {
		announce("built into this binary", "embedded; pass --"+flagName+" to use your own")
	}
	return embedded, cleanupEmbedded, embedErr
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// extractEmbeddedDefaults materializes balise.EmbeddedDefaults onto a fresh temp
// directory and returns the path to its "defaults" subtree — the same shape every
// other resolveDefaultsDir case returns (a directory directly containing order.yaml,
// types/, facets/, and so on). Materializing to disk, rather than teaching every
// registry loader to read an fs.FS, keeps this fix local to cmd/balise: registry.Load,
// LoadOrder, LoadFacets, LoadSpaces, LoadTenants and LoadDeclaredScopes all read through
// os.ReadFile/filepath.Glob against a real path today, and still do after this change.
//
// The returned cleanup relies on the caller's defer running, which it does on every
// normal exit path (including an MCP host closing stdin, which is how RunStdio's
// server.Run returns and lets mcpCmd's RunE reach its defers). A hard kill (SIGKILL, or
// any signal whose default disposition terminates the process before Go unwinds
// deferred calls) skips it and leaks the temp directory -- the same accepted tradeoff
// any os.MkdirTemp caller has under an ungraceful exit. The leaked directory holds
// nothing beyond the tracked, generic defaults/ subset embedded above, so the worst
// case is a harmless stray temp directory, never a data or security exposure.
func extractEmbeddedDefaults() (string, func(), error) {
	tmp, err := os.MkdirTemp("", "balise-defaults-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir for embedded defaults: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	walkErr := fs.WalkDir(balise.EmbeddedDefaults, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(tmp, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, readErr := fs.ReadFile(balise.EmbeddedDefaults, path)
		if readErr != nil {
			return fmt.Errorf("read embedded %s: %w", path, readErr)
		}
		return os.WriteFile(target, body, 0o644)
	})
	if walkErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("extract embedded defaults: %w", walkErr)
	}
	return filepath.Join(tmp, "defaults"), cleanup, nil
}
