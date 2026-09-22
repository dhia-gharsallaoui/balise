<div align="center">

# Balise

**A knowledge base whose primary reader is an agent, not a person.**

[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React 19](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![Postgres 15](https://img.shields.io/badge/Postgres-15-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![MCP](https://img.shields.io/badge/MCP-6%20tools-6E56CF)](https://modelcontextprotocol.io)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Your notes are markdown files in a git repo. Balise indexes them into Postgres, serves them to
AI agents over MCP with per-agent scoping, and gives you a web UI to review what the agents want
to change — before anything is written.

<img src="docs/media/tour.gif" alt="Searching claims for 'failover', opening the matching page in the reader, then switching to the relation graph" width="800">

</div>

---

## Why

Agent memory is usually one long file that grows until it is mostly noise, or a vector store you
cannot read, edit, or reason about. Both fail the same way: you cannot tell what the agent knows,
you cannot correct it, and you cannot stop it from telling one client's secrets to another.

Balise takes the opposite position on all three.

|  |  |
|---|---|
| **You can read it** | Every page is markdown on disk. `git log` is the audit trail. Delete the database and `balise reindex` rebuilds it — Postgres holds nothing the vault does not. |
| **You can correct it** | Agents do not write to pages. They write *proposals*, and you accept, edit-then-accept, or reject. |
| **It cannot leak** | Scope is a compile-time boundary, not a convention. A scopeless query does not compile. |

## Propose, don't mutate

An agent found something and wants to add it. It does not touch the page — it files a proposal
with its evidence, and you decide.

<img src="docs/media/review.gif" alt="Accepting a proposal in the Review queue: the evidence and the current-versus-proposed claims are shown side by side, Accept is pressed, and the queue advances to the next proposal" width="800">

<sup>Still frame, if the GIF does not play: [light](docs/media/review.png) · [dark](docs/media/review-dark.png)</sup>

Accepting commits to git as `agent:compile`. Editing before you accept records that the wording
changed, so **proposal → your edit → commit** stays traceable. Nothing an LLM produces reaches a
page without someone saying yes.

## Five ideas worth knowing

**Claims, not documents.** A page carries 1–6 claims: short statements, not subjects. "Samples
older than 20 minutes are rejected" is a claim; "retention policy" is a heading. Search and
context assembly work over claims, so an agent retrieves the assertion it needs rather than a
chunk that mentions the topic.

**Scope is enforced by construction.** `store.Queries` binds its scope list when it is built and
every query filters on it — a scopeless query does not compile. AST guard tests enforce that as
an *allowlist*, not a blocklist; they were inverted after reviewers built working bypasses
against the blocklist form. A token scoped to `work` asking for a `client-acme` page gets
`not found`, identical to a slug that never existed, so absence and denial are indistinguishable.

**Two credential systems, deliberately not merged.** A session cookie authenticates one owner at
`/api`. Agent tokens — with their own scopes, capabilities, revocation and audit trail —
authenticate `/mcp`. A cookie cannot drive a tool call; a token cannot read the UI API.

**Findings resolve themselves.** Lint findings ("this headline is too long", "this link points
nowhere") clear when the cause is fixed and the page is reindexed. A to-do list that only grows
is noise.

**Everything derived is rebuildable.** The vault is the source of truth. The index, the graph,
the search tables — all of it regenerates from markdown and git.

---

## Quickstart

Fastest way to see it running, with no data of your own:

```bash
git clone https://github.com/dhia-gharsallaoui/balise.git && cd balise
make demo
```

`make demo` builds a small fictional vault, indexes it into its own database schema, and prints
the exact command to start the app against it. It never touches your own vault or index, and it
is generated deterministically — rerunning it reproduces the same pages, claims and git history
byte for byte.

<table>
<tr>
<td width="50%"><img src="docs/media/home.png" alt="The Home screen: a greeting, four claim proposals waiting for a decision grouped by space, two lint findings needing attention, and a list of recently changed pages"></td>
<td width="50%"><img src="docs/media/knowledge.png" alt="The Knowledge screen: a space tree with per-space page counts on the left and claim-level search results grouped by page type"></td>
</tr>
<tr>
<td><img src="docs/media/graph.png" alt="The relation graph: one panel per scope, each laid out as a radial spanning tree, with pages having no relations shelved separately below"></td>
<td><img src="docs/media/reader.png" alt="A page open in the full-width reading view, with its claims listed above the body text and its metadata and backlinks below"></td>
</tr>
</table>

Dark mode is a first-class theme, not an inversion —
[Home](docs/media/home-dark.png) · [Knowledge](docs/media/knowledge-dark.png) ·
[Graph](docs/media/graph-dark.png) · [Review](docs/media/review-dark.png).

### Against your own data

```bash
cp .env.example .env          # set BALISE_PASSWORD
docker compose up -d          # Postgres on :5433, or bring your own
./bin/balise init ~/my-vault  # a git repo with the expected layout
make up VAULT=~/my-vault DSN=postgresql://balise:balise@localhost:5433/balise
```

Open **http://localhost:5173**. Then `make status`, `make logs`, `make down`, `make test`.

`make up` starts the API on `127.0.0.1:8099` and the UI on `127.0.0.1:5173`. Vite proxies `/api`
over loopback, so **only one port is ever exposed** and requests stay same-origin.

### What a page looks like

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

Eight types ship by default — `state`, `decision`, `gotcha`, `procedure`, `issue`, `incident`,
`note`, `entity` — each with its own fields and staleness rule. They are YAML in
`defaults/types/`, not code; add your own.

---

## Giving an agent access

Mint a token scoped to exactly what that agent should see:

```bash
./bin/balise token create my-assistant --scopes work,client-acme --capabilities read,remember
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

Use the env var rather than `--token`: a flag value is visible to any process via `ps` and lands
in shell history.

| Tool | What it does |
|---|---|
| `context` | Assembles a context pack to a token budget. Oversize pages come back in `unloaded[]` with a reason rather than silently truncated. |
| `search` | Claim-level search across the token's scopes. Reports `coverage: ok\|low`. |
| `pages` | Lists pages by type, with optional scope, tags, status. |
| `fetch` | One page with its claims, by scope and slug. |
| `related` | Relations for a page, grouped by role, depth-clamped. |
| `remember` | Appends to the agent's own memory file. Needs the `remember` capability. |

Every call writes one audit row — **including refusals**. The Agents screen shows which spaces
each agent may read, what it actually did, and distinguishes a refused read from a successful one.

## Architecture

```mermaid
flowchart TD
    V["markdown vault<br/><i>git — source of truth</i>"] --> I[indexer]
    I --> P[("Postgres<br/><i>derived, rebuildable</i>")]
    P --> A["REST /api<br/><i>session cookie · 1 owner</i>"]
    P --> M["MCP /mcp<br/><i>bearer tokens · N agents</i>"]
    A --> U[React web UI]
    M --> G[agents]
    G -. proposals .-> R["review/"]
    R -. you accept .-> V
```

Go 1.26 · pgx · go-git · goldmark · Postgres 15 (ltree, pg_trgm, unaccent) · React 19 · Vite ·
TypeScript

`internal/store/queries.go` is the only file containing SQL. The indexer is idempotent:
reindexing an unchanged vault changes nothing, and a malformed page produces a lint finding
rather than an error.

## Configuration

`defaults/` declares types, facets, spaces and tenants. Three files name real tenants and are
**not tracked**:

```
defaults/tenants.yaml          defaults/tenants.example.yaml
defaults/spaces.yaml           defaults/spaces.example.yaml
defaults/facets/customer.yaml  defaults/facets/customer.example.yaml
```

The loader falls back to the `.example` sibling when the real file is absent, so a fresh clone
runs out of the box. Copy and edit when you are ready.

The Settings screen shows what the registry currently declares, and which agents can read each
space — the same scopes that gate `/mcp`:

<img src="docs/media/settings.png" alt="The Settings screen listing each page type with its fields, staleness rule and live page count, the tag facets in use, and the relation kinds present in the vault" width="830">

### Exposing beyond loopback

`make expose` binds the UI to every interface for port-forwarding. The API **refuses to start**
on a non-loopback address unless `BALISE_PASSWORD` is set — by design, so that hole cannot reopen
because a flag was forgotten.

There is no TLS here. Over plain HTTP the password crosses the wire in the clear; put this behind
SSH, a tunnel, or a firewall rule.

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

## What it does not do yet

Named here rather than discovered later.

- **No export, no redaction.** The Settings controls are visibly disabled. Choosing an audience
  and previewing what redaction strips is designed but not built, and a preview that did not
  really redact would be worse than none on a vault holding several tenants.
- **No connectors.** Nothing syncs from an external system on a schedule. Pages arrive by import,
  by `remember`, or by pasting into Sources.
- **Retrieval is lexical.** No embedding model, no vector index. `search` and `context` rank with
  Postgres full-text and trigram matching, and `context` reports `coverage: low` when it knows it
  did badly rather than implying a confidence it cannot support.
- **Single owner.** One password, no accounts, roles or sharing.
- **Rate limiting is per process.** It does not coordinate across instances.

## Testing

```bash
make test
```

Go tests run with `-race`. The e2e suite drives a real browser against a real backend and a real
Postgres; acceptance tests needing a seed corpus skip cleanly when `BALISE_ACCEPTANCE_CORPUS` is
unset.

The screenshots and GIFs above are regenerated from the demo vault by
`node web/tools/capture-media.mjs`, so they cannot drift from the UI without someone noticing.

## License

MIT — see [LICENSE](LICENSE).
