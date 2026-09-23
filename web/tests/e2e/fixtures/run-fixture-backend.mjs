#!/usr/bin/env node
// Assembles and serves the frozen fixture vault used by visual.spec.ts, so those
// screenshots stop drifting against whatever /tmp/balise-live happens to contain on a
// given run (see task 3 of the 2026-09-16 home-report). This script:
//
//   1. builds the balise binary once,
//   2. creates a scratch git-backed vault and copies the committed fixture content
//      (web/tests/e2e/fixtures/vault/) into it,
//   3. commits that content with a fixed author/committer date so the git history the
//      "Changed recently" block reads is itself deterministic,
//   4. reindexes it into the dedicated `fixture_vault` Postgres schema, and
//   5. execs `balise serve` on a distinct port (127.0.0.1:8199), forwarding this
//      process's own termination signals so Playwright's webServer teardown kills the
//      Go server cleanly instead of orphaning it.
//
// It is a Node script rather than a bash wrapper specifically so step 5's child process
// can be tracked and signalled directly (child_process.spawn + kill) instead of relying on
// bash job control / trap plumbing to forward signals through an extra shell layer.

import { execFileSync, spawn } from "node:child_process";
import { cpSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = join(__dirname, "..", "..", "..", "..");
const FIXTURE_SOURCE = join(__dirname, "vault");
// Deliberately NOT REPO_ROOT/defaults. That directory holds the *developer's own*
// registry — defaults/spaces.yaml and tenants.yaml are gitignored because they name real
// clients — so pointing the fixture backend at it baked whatever scopes that developer
// happened to have into the visual baselines. On the author's machine that leaked real
// client names into committed screenshots; on anyone else's it produced a render that
// could never match the committed baseline, since a fresh clone only has the .example
// files. Passing no --defaults resolves to the binary's embedded copy (see
// cmd/balise/defaults.go), which is the tracked, generic set and identical everywhere.
const DEFAULTS_ARGS = [];

const DSN = "postgresql://balise:balise@localhost:5432/balise?search_path=fixture_vault,public";
const ADDR = "127.0.0.1:8199";

// Fixed commit timestamp: the vault is rebuilt from scratch on every run, and a
// timestamp of "now" would make the git history ("Changed recently") a new set of
// commits on every run — exactly the flakiness this fixture exists to remove.
const FIXED_DATE = "2026-01-15T09:00:00Z";

function run(cmd, args, options = {}) {
  execFileSync(cmd, args, { stdio: "inherit", cwd: REPO_ROOT, ...options });
}

function buildBinary() {
  const binPath = join(mkdtempSync(join(tmpdir(), "balise-fixture-bin-")), "balise");
  run("go", ["build", "-o", binPath, "./cmd/balise"]);
  return binPath;
}

function assembleVault(binPath) {
  const vaultDir = join(tmpdir(), "balise-fixture-vault");
  rmSync(vaultDir, { recursive: true, force: true });
  run(binPath, ["init", vaultDir]);
  cpSync(FIXTURE_SOURCE, vaultDir, { recursive: true });

  const gitEnv = {
    ...process.env,
    GIT_AUTHOR_NAME: "fixture-vault",
    GIT_AUTHOR_EMAIL: "fixture-vault@localhost",
    GIT_AUTHOR_DATE: FIXED_DATE,
    GIT_COMMITTER_NAME: "fixture-vault",
    GIT_COMMITTER_EMAIL: "fixture-vault@localhost",
    GIT_COMMITTER_DATE: FIXED_DATE,
  };
  run("git", ["-C", vaultDir, "add", "-A"], { env: gitEnv });
  run(
    "git",
    ["-C", vaultDir, "-c", "user.name=fixture-vault", "-c", "user.email=fixture-vault@localhost",
      "commit", "-m", "fixture vault: frozen content for visual regression tests"],
    { env: gitEnv },
  );
  return vaultDir;
}

function reindex(binPath, vaultDir) {
  run(binPath, ["reindex", vaultDir, "--dsn", DSN, ...DEFAULTS_ARGS]);
}

function serve(binPath, vaultDir) {
  // No explicit `env` here: Node's child_process.spawn defaults to inheriting this process's
  // own process.env, so whatever BALISE_PASSWORD playwright.config.ts set on this script's
  // webServer entry reaches `balise serve` unchanged — internal/api/auth.go then requires a
  // session for every /api route on :8199 exactly as it does in production.
  const child = spawn(
    binPath,
    ["serve", vaultDir, "--addr", ADDR, "--dsn", DSN, ...DEFAULTS_ARGS],
    { stdio: "inherit", cwd: REPO_ROOT },
  );

  // Forward termination so Playwright's webServer teardown actually stops the Go
  // process instead of leaving it bound to :8199 after this wrapper exits.
  for (const signal of ["SIGTERM", "SIGINT"]) {
    process.on(signal, () => {
      child.kill(signal);
    });
  }
  child.on("exit", (code) => {
    process.exit(code ?? 0);
  });
}

function main() {
  const binPath = buildBinary();
  const vaultDir = assembleVault(binPath);
  reindex(binPath, vaultDir);
  serve(binPath, vaultDir);
}

main();
