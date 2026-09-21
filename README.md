# Balise

**A knowledge base whose primary reader is an agent, not a person.**

Your notes are markdown files in a git repo. Balise indexes them into Postgres,
serves them to AI agents over MCP with per-agent scoping, and gives you a web UI
to review what the agents want to change before anything is written.

```
vault/                        MCP tools                 web UI
  work/                         context    ─┐             Home      what needs you
    gotchas/*.md                search      │             Knowledge browse · search · graph
    decisions/*.md   ──▶ index ─ fetch      ├─▶ agent     Review    accept · edit · reject
  client-acme/                  pages       │             Sources   add pages
    issues/*.md                 related     │             Agents    who may read what
  review/*.md                   remember   ─┘             Settings  types · scopes · storage
```

---

## Why

Agent memory is usually one long file that grows until it is mostly noise, or a
vector store you cannot read, edit, or reason about. Both fail the same way: you
cannot tell what the agent knows, you cannot correct it, and you cannot stop it
from telling one client's secrets to another.

Balise takes the opposite position on all three.

**You can read it.** Every page is markdown on disk. `git log` is the audit
trail. Delete the database and `balise reindex` rebuilds it — Postgres holds
nothing the vault does not.

**You can correct it.** Agents do not write to pages. They write *proposals*
into `review/`, and you accept, edit-then-accept, or reject. An edited accept
records that the wording changed before it was committed, so proposal → your
edit → commit stays traceable.

**It cannot leak across clients.** Scope is a compile-time boundary, not a
convention — see below.

## Five ideas worth knowing

**Claims, not documents.** A page carries 1–6 claims: short statements, not
subjects. "Samples older than 20 minutes are rejected" is a claim; "retention
policy" is a heading. Search and context assembly work over claims, so an agent
retrieves the assertion it needs rather than a chunk that mentions the topic.

**Scope is enforced by construction.** `store.Queries` binds its scope list when
it is built and every query filters on it — a scopeless query does not compile.
AST guard tests enforce that as an *allowlist*, not a blocklist; they were
inverted after reviewers built working bypasses against the blocklist form. A
token scoped to `work` asking for a `client-acme` page gets `not found`,
identical to a slug that never existed, so absence and denial are
indistinguishable.

**Propose, don't mutate.** Compile tasks write proposals; humans decide. Nothing
an LLM produces reaches a page without someone accepting it.

**Two credential systems, deliberately not merged.** A session cookie
authenticates one owner at `/api`. Agent tokens — with their own scopes,
capabilities, revocation and audit trail — authenticate `/mcp`. A cookie cannot
drive a tool call; a token cannot read the UI API. Different lifetimes,
different blast radius.

**Findings resolve themselves.** Lint findings ("this headline is too long",
"this link points nowhere") clear when the cause is fixed and the page is
reindexed. A to-do list that only grows is noise.

---

## Quickstart

```bash
git clone https://github.com/dhia-gharsallaoui/balise.git && cd balise
cp .env.example .env          # set BALISE_PASSWORD
docker compose up -d          # Postgres on :5433, or bring your own
make up DSN=postgresql://balise:balise@localhost:5433/balise
```

Open **http://localhost:5173**.

```bash
make status   # what's running and where
make logs     # tail both logs
make down     # stop
make test     # go test -race, vitest, production build, playwright
```

`make up` builds the binary, installs web dependencies, and starts the API on
`127.0.0.1:8099` and the UI on `127.0.0.1:5173`. Vite proxies `/api` over
loopback, so **only one port is ever exposed** and requests stay same-origin.

### A vault of your own

```bash
./bin/balise init ~/my-vault      # a git repo with the expected layout
make up VAULT=~/my-vault
```

Pages are markdown with YAML frontmatter:

```markdown
---
type: gotcha
scope: work
title: Apply Cognitive Services changes with -parallelism=1
tags: [vendor/azure, layer/iac]
claims:
  - text: Concurrent deployment PUTs return 409 RequestConflict
    status: active
---

Three deployments in parallel failed; the same three serialized succeeded.
```

Eight types ship by default — `state`, `decision`, `gotcha`, `procedure`,
`issue`, `incident`, `note`, `entity` — each with its own fields and staleness
rule. They are YAML in `defaults/types/`, not code; add your own.

---

## Giving an agent access

Mint a token scoped to exactly what that agent should see:

```bash
./bin/balise token create my-assistant --scopes work,client-acme \
    --capabilities read,remember
```

The token is printed once. Point Claude Code at it over stdio:

```json
{
  "mcpServers": {
    "balise": {
      "command": "/path/to/balise",
      "args": ["mcp", "--stdio", "/path/to/vault"],
      "env": { "BALISE_MCP_TOKEN": "<the token>" }
    }
  }
}
```

Use the env var rather than `--token`: a flag value is visible to any process
via `ps` and lands in shell history.

### The tools

| Tool | What it does |
|---|---|
| `context` | Assembles a context pack to a token budget. Oversize pages come back in `unloaded[]` with a reason rather than silently truncated. |
| `search` | Claim-level search across the token's scopes. Reports `coverage: ok\|low`. |
| `pages` | Lists pages by type, with optional scope, tags, status. |
| `fetch` | One page with its claims, by scope and slug. |
| `related` | Relations for a page, grouped by role, depth-clamped. |
| `remember` | Appends to the agent's own memory file. Needs the `remember` capability. |

Every call writes one `audit_log` row — including refusals. The Agents screen
shows what each agent actually did, and distinguishes a refused read from a
successful one.

---

## Architecture

```
markdown vault (git)  ──▶  indexer  ──▶  Postgres (derived, rebuildable)
                                            │
                              ┌─────────────┴─────────────┐
                         REST /api                    MCP /mcp
                     session cookie, 1 owner    bearer tokens, N agents
                              │                            │
                         React web UI                   agents
```

Go 1.26 · pgx · go-git · goldmark · Postgres 15 (ltree, pg_trgm, unaccent) ·
React 18 · Vite · TypeScript

`internal/store/queries.go` is the only file containing SQL. The indexer is
idempotent: reindexing an unchanged vault changes nothing, and a malformed page
produces a lint finding rather than an error.

## Configuration

`defaults/` declares types, facets, spaces and tenants. Three files name real
tenants and are **not tracked**:

```
defaults/tenants.yaml          defaults/tenants.example.yaml
defaults/spaces.yaml           defaults/spaces.example.yaml
defaults/facets/customer.yaml  defaults/facets/customer.example.yaml
```

The loader falls back to the `.example` sibling when the real file is absent, so
a fresh clone runs against example tenants out of the box. Copy and edit when
you are ready:

```bash
cp defaults/tenants.example.yaml defaults/tenants.yaml
```

### Exposing beyond loopback

`make expose` binds the UI to every interface for port-forwarding. The API
**refuses to start** on a non-loopback address unless `BALISE_PASSWORD` is set —
by design, so that hole cannot reopen because a flag was forgotten.

There is no TLS here. Over plain HTTP the password crosses the wire in the
clear; put this behind SSH, a tunnel, or a firewall rule.

## CLI

```
balise init <vault>                       create an empty vault
balise import <vault> --corpus D          import a Claude Code memory directory
balise reindex <vault>                    rebuild the index from the vault
balise serve <vault>                      REST API + MCP endpoint
balise mcp <vault> --stdio                standalone stdio MCP session
balise token create|list|revoke <name>    manage agent tokens
balise compile extract-claims <vault>     propose claims for pages missing them
```

`--dsn` (or `BALISE_DSN`) applies to all of them.

---

## What it does not do yet

Named here rather than discovered later.

- **No export, no redaction.** The Settings controls are visibly disabled.
  Choosing an audience and previewing what redaction strips is designed but not
  built, and a preview that did not really redact would be worse than none on a
  vault holding several tenants.
- **No connectors.** Nothing syncs from an external system on a schedule. Pages
  arrive by import, by `remember`, or by pasting into Sources. `list_sources` is
  specified but unimplementable against the current schema, so it is not
  exposed.
- **Retrieval is lexical.** No embedding model, no vector index. `search` and
  `context` rank with Postgres full-text and trigram matching. `context` reports
  `coverage: low` when it knows it did badly rather than implying a confidence
  it cannot support.
- **Single owner.** One password, no accounts, roles or sharing.
- **Rate limiting is per process.** It does not coordinate across instances.

## Testing

```bash
make test
```

Go tests run with `-race`. The e2e suite drives a real browser against a real
backend and a real Postgres; acceptance tests that need a seed corpus skip
cleanly when `BALISE_ACCEPTANCE_CORPUS` is unset.

## License

MIT — see [LICENSE](LICENSE).
