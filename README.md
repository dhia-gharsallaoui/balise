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
`note`, `memory` — each with its own fields and staleness rule. They are YAML in
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

### Teaching the agent to use it

Connecting the server is half of it. An agent with these tools available will read when you
ask it to and otherwise ignore them — so the vault only ever grows when *you* feed it.

[`skill/SKILL.md`](skill/SKILL.md) closes that loop. It gives the agent two habits: call
`context` before answering anything about these systems, and offer to `remember` whatever it
establishes that is durable and not already recorded. It also tells it what is *not* worth
writing — anything already there, anything true for ten minutes, anything it inferred but did
not verify — and that a client's detail belongs in that client's scope and nowhere else.

```bash
mkdir -p ~/.claude/skills/balise
ln -s "$PWD/skill/SKILL.md" ~/.claude/skills/balise/SKILL.md
```

It is one plain markdown file with YAML frontmatter, which is the format Claude Code, Codex
and OpenCode all read — nothing in it is specific to one host. See
[`skill/README.md`](skill/README.md) for per-host paths and what is worth tuning.

**Mint the token with `remember`, not just `read`.** An agent following this skill with a
read-only token will be refused every time it tries to record something, which looks like
the skill misbehaving and is really the token being too narrow.

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

Commands that read the registry also take `--defaults`. When it is not given they look
inside the vault, then beside it, then fall back to the copy embedded in the binary — the
working directory is never consulted, which is what lets an MCP host spawn `balise` from
anywhere. Every run prints which one it picked, on stderr:

```
defaults: built into this binary (embedded; pass --defaults to use your own)
```

If you are editing `defaults/` in a checkout, pass `--defaults ./defaults` or that line will
tell you your edits were ignored.

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
