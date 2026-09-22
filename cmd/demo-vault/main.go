// Command demo-vault generates the Balise demo vault: a small, entirely fictional git
// repository of pages (roughly 40 across a shared "work" scope and three client scopes) plus
// a handful of pending review proposals, used for README screenshots/GIFs and for anyone
// exploring the app without a real client's data.
//
// It is deterministic and re-runnable by construction, not by convention: every page uses a
// hardcoded uid (page.go's uid()), every claim id and date is a literal in content_*.go, and
// every git commit below fixes its own author, committer, email and timestamp instead of
// reading time.Now() — so two runs against a clean output directory produce byte-identical
// working trees and, since the commit metadata is fixed too, identical commit SHAs. This is
// also why it shells out to the `git` CLI (mirroring
// web/tests/e2e/fixtures/run-fixture-backend.mjs's own gitEnv pattern) instead of using
// store.GitPageStore.Commit, which hardcodes time.Now() internally and so cannot produce a
// reproducible history.
//
// The five commit authors below (Dhia, Marcus Webb, Priya Nair, Sana Idris, Leo Ferreira) and
// every "@platform.example" address are invented for this vault; ".example" is the address
// reserved for documentation use by RFC 2606, so none of it resolves to anything real. This
// matters because the sandbox's global git config carries the real repo owner's personal
// identity — every commit below explicitly overrides both author and committer via
// GIT_AUTHOR_*/GIT_COMMITTER_* env vars *and* `-c user.name=`/`-c user.email=` flags, so that
// identity can never leak into this vault's published history.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// author is one invented commit identity.
type author struct{ name, email string }

var (
	authorDhia   = author{"Dhia", "dhia@platform.example"}
	authorMarcus = author{"Marcus Webb", "marcus.webb@platform.example"}
	authorPriya  = author{"Priya Nair", "priya.nair@platform.example"}
	authorSana   = author{"Sana Idris", "sana.idris@platform.example"}
	authorLeo    = author{"Leo Ferreira", "leo.ferreira@platform.example"}
)

// commitGroup is one batch of pages (or review proposals) written to disk and then committed
// together, with its own author and fixed date. Splitting the ~40 pages plus 4 proposals into
// nine groups (instead of one "initial import" commit) is what gives Home's recent-commits
// list and the Sources ingest log something real to show, spanning early August through late
// September 2026, across three of authorWord's four message-prefix buckets — "balise:" (a
// connector), "compile:" (an agent) and everything else (you).
type commitGroup struct {
	message string
	by      author
	date    string // RFC3339; used for both GIT_AUTHOR_DATE and GIT_COMMITTER_DATE
	pages   []page
	reviews []reviewProposal
}

func commitGroups() []commitGroup {
	work := workPages()
	return []commitGroup{
		{
			message: "chore: seed the shared ExpressRoute transit hub and AKS fleet entities",
			by:      authorDhia, date: "2026-08-04T10:00:00Z",
			pages: work[0:4], // expressroute-transit, shared-aks-fleet, both node-image states
		},
		{
			message: "docs: capture the IaC and AKS fleet gotchas engineers keep re-discovering",
			by:      authorMarcus, date: "2026-08-07T14:30:00Z",
			pages: work[4:7], // node pool taints, terraform for_each, azfw policy import
		},
		{
			message: "feat: adopt the shared fleet and per-subscription workspace decisions",
			by:      authorDhia, date: "2026-08-15T09:15:00Z",
			pages: work[7:10], // terraform workspaces, aks fleet model, dual-circuit standard
		},
		{
			message: "balise: sync onboarding and credential-rotation procedures from the runbook repo",
			by:      authorMarcus, date: "2026-08-20T16:45:00Z",
			pages: work[10:12], // onboard subscription, rotate fleet credentials
		},
		{
			message: "chore: log open platform issues and the august pipeline incident",
			by:      authorDhia, date: "2026-08-25T11:20:00Z",
			pages: work[12:16], // drift detector, capacity headroom, pipeline outage, on-call note
		},
		{
			message: "feat: onboard Acme retail SD-WAN fabric knowledge",
			by:      authorPriya, date: "2026-09-01T13:00:00Z",
			pages: acmePages(),
		},
		{
			message: "feat: onboard Globex ExpressRoute and storage knowledge",
			by:      authorSana, date: "2026-09-09T10:30:00Z",
			pages: globexPages(),
		},
		{
			message: "feat: onboard Initech shared compute and storage baseline",
			by:      authorLeo, date: "2026-09-14T15:10:00Z",
			pages: initechPages(),
		},
		{
			message: "compile: extract candidate claim updates for review",
			by:      authorDhia, date: "2026-09-20T09:00:00Z",
			reviews: reviewProposals(),
		},
	}
}

func main() {
	vaultDir := flag.String("vault", "demo-vault", "output path for the generated vault (created fresh each run)")
	defaultsDir := flag.String("defaults", "demo-vault-defaults", "output path for the demo's own defaults directory")
	sourceDefaults := flag.String("source-defaults", "defaults", "the repo's real defaults directory to copy generic files from")
	flag.Parse()

	if err := run(*vaultDir, *defaultsDir, *sourceDefaults); err != nil {
		fmt.Fprintln(os.Stderr, "demo-vault:", err)
		os.Exit(1)
	}
}

func run(vaultDir, defaultsDir, sourceDefaults string) error {
	vaultDir, err := filepath.Abs(vaultDir)
	if err != nil {
		return fmt.Errorf("resolve vault path: %w", err)
	}
	defaultsDir, err = filepath.Abs(defaultsDir)
	if err != nil {
		return fmt.Errorf("resolve defaults path: %w", err)
	}

	// Regenerate from scratch every run — the whole point is that a second run produces the
	// same vault, not a vault plus leftovers from the last one.
	if err := os.RemoveAll(vaultDir); err != nil {
		return fmt.Errorf("clean %s: %w", vaultDir, err)
	}
	if err := os.RemoveAll(defaultsDir); err != nil {
		return fmt.Errorf("clean %s: %w", defaultsDir, err)
	}
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", vaultDir, err)
	}

	if err := writeDefaults(sourceDefaults, defaultsDir); err != nil {
		return fmt.Errorf("write defaults: %w", err)
	}

	if err := gitInit(vaultDir); err != nil {
		return err
	}

	for _, g := range commitGroups() {
		paths, err := writeGroup(vaultDir, g)
		if err != nil {
			return fmt.Errorf("write group %q: %w", g.message, err)
		}
		if err := gitCommit(vaultDir, paths, g); err != nil {
			return fmt.Errorf("commit group %q: %w", g.message, err)
		}
	}

	fmt.Printf("demo vault written to %s (defaults: %s)\n", vaultDir, defaultsDir)
	return nil
}

// writeGroup writes every page and review proposal in g to disk and returns their
// vault-relative paths, for `git add`.
func writeGroup(vaultDir string, g commitGroup) ([]string, error) {
	var paths []string
	for _, p := range g.pages {
		rel := p.path()
		if err := writeFile(vaultDir, rel, p.render()); err != nil {
			return nil, err
		}
		paths = append(paths, rel)
	}
	for _, rp := range g.reviews {
		content, err := rp.render()
		if err != nil {
			return nil, err
		}
		if err := writeFile(vaultDir, rp.path, content); err != nil {
			return nil, err
		}
		paths = append(paths, rp.path)
	}
	return paths, nil
}

func writeFile(vaultDir, rel, content string) error {
	full := filepath.Join(vaultDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", full, err)
	}
	return nil
}

// gitInit creates the vault's own git repository on a fixed branch name ("main") regardless
// of the environment's init.defaultBranch setting, so the branch name is as deterministic as
// everything else here.
func gitInit(vaultDir string) error {
	return runGit(vaultDir, nil, "init", "-q", "-b", "main")
}

// gitCommit stages paths and commits them as g's author/committer at g's fixed date. Both the
// env vars and the -c flags below override author *and* committer identity — belt and
// braces, matching web/tests/e2e/fixtures/run-fixture-backend.mjs's gitEnv pattern exactly —
// so the sandbox's global `user.name`/`user.email` (the real repo owner's personal identity)
// can never leak into a commit this generator makes.
func gitCommit(vaultDir string, paths []string, g commitGroup) error {
	addArgs := append([]string{"add", "--"}, paths...)
	if err := runGit(vaultDir, nil, addArgs...); err != nil {
		return err
	}

	env := []string{
		"GIT_AUTHOR_NAME=" + g.by.name,
		"GIT_AUTHOR_EMAIL=" + g.by.email,
		"GIT_AUTHOR_DATE=" + g.date,
		"GIT_COMMITTER_NAME=" + g.by.name,
		"GIT_COMMITTER_EMAIL=" + g.by.email,
		"GIT_COMMITTER_DATE=" + g.date,
	}
	return runGit(vaultDir, env,
		"-c", "user.name="+g.by.name,
		"-c", "user.email="+g.by.email,
		"commit", "-q", "-m", g.message,
	)
}

func runGit(vaultDir string, extraEnv []string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", vaultDir}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}
