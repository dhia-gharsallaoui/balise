# The Balise skill

`SKILL.md` teaches an agent the two habits that make a Balise vault worth having: read from
it before answering, and write back to it when it learns something durable.

Without this, an agent connected over MCP will happily read and never write — the vault only
grows when a human feeds it, and the loop never closes.

It is a single plain markdown file with YAML frontmatter, deliberately. That format is what
Claude Code, Codex and OpenCode all understand, so one file covers every host.

## Install

**Claude Code** — copy or symlink it into your skills directory:

```bash
mkdir -p ~/.claude/skills/balise
ln -s "$PWD/skill/SKILL.md" ~/.claude/skills/balise/SKILL.md
```

Per-project instead of global: `.claude/skills/balise/SKILL.md` in the repo.

**Codex / OpenCode** — both read skill files from their own skills directory; drop the same
`SKILL.md` in, under a `balise/` folder. Nothing in it is Claude-specific.

**Any other host** — the file is prose. If your agent takes a system prompt and nothing
else, paste the body in.

## It needs the MCP server

The skill describes tools; it does not provide them. Connect the vault first, or the agent
will be told to call `context` and find nothing to call:

```bash
balise token create my-assistant --scopes work --capabilities read,remember
```

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

**The `remember` capability is what makes the writing half work.** A token minted with only
`read` will follow the skill's reading habits and be refused every time it tries to record
anything — which looks like the skill misbehaving and is really the token being too narrow.

Scopes are the other half. An agent can only write where it can read, so a token scoped to
`work` cannot file anything under a client scope, by design.

## Tuning it

Edit `SKILL.md`; it is meant to be edited. The parts most worth adjusting:

- **The "what is worth remembering" list.** It is written for infrastructure and platform
  work. If your vault is about something else, the examples should be about that.
- **The offer.** It asks before writing. If you would rather it write unprompted for a
  trusted agent, say so there — but consider that a review queue exists precisely because
  unattended writes are hard to audit.
- **The fifteen-word guidance.** That is Balise's claim convention. If you keep longer
  claims, change it here too or the agent will keep truncating usable detail.
