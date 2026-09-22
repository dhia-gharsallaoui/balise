---
name: balise
description: Use when a Balise vault is connected over MCP. Read from it before answering questions about this codebase, these systems, or past decisions, and write back to it when you establish something durable that is not already recorded.
---

# Balise

A Balise vault is connected over MCP. It holds what has already been learned about these
systems — decisions, gotchas, incidents, procedures — as markdown pages carrying short
factual **claims**.

Two habits. Read before you answer. Write when you learn.

## Read first

Before answering anything about this codebase, this infrastructure, how something was
configured, or why something was decided, **call `context` first**. The answer may already
be there, written down by someone who debugged it properly.

```
context(query: "why does the apply fail on shared infrastructure", budget_tokens: 6000)
```

Use `context` for questions. It assembles a token-budgeted pack of the most relevant claims
plus the pages they specialize or supersede. Use `search` when you want hits to triage
yourself, `fetch` when you already know the page, `related` to walk from one page to its
neighbours, and `pages` to list a type.

**Read `coverage` before you trust the result.** `coverage: "low"` means the vault did not
have much for that query — say so rather than presenting a thin pack as settled knowledge.
Ranking here is lexical, not semantic: matching words, not matching meaning. A query using
different vocabulary than the vault can miss real content, so if a first `context` call
comes back thin, try the words the systems actually use before concluding nothing is there.

If the vault does have the answer, **use it and say where it came from**. That is the
difference between an answer and a guess.

## Write when you learn something

When you establish something durable that the vault does not already hold, offer to record
it with `remember`. Ask first — one line, not a ceremony — and write on a yes.

> That behaviour isn't in the vault. Want me to record it?

```
remember(text: "Concurrent deployment PUTs against one account return 409; serialise them", scope: "work")
```

### What is worth remembering

A claim that will still be true and still be useful in six months, that someone would
otherwise have to rediscover the hard way:

- **A gotcha.** A system behaved in a surprising way and you worked out why.
- **A decision.** An approach was chosen over alternatives, and the reasoning matters.
- **A correction.** Something the vault says is wrong or now out of date.
- **A hard-won fact.** A limit, a required flag, an ordering constraint that cost real time
  to find.

Write it as a **statement, not a subject**. "Samples older than twenty minutes are rejected"
is a claim. "Retention policy" is a heading, and a heading tells a future reader nothing.
Keep it short — roughly fifteen words. Say what is true, not what you did.

### What is not

- Anything already in the vault. You read it first; do not write it back.
- Anything true only for the next ten minutes — a running command, a temporary workaround,
  the state of a branch.
- Anything you inferred but did not verify. A guess recorded as a claim is worse than no
  claim, because the next reader cannot tell the difference.
- Secrets, credentials, tokens, customer personal data. The vault is not a password store.
- A summary of the conversation. Record the finding, not the transcript.

### Scope is a boundary, not a filing preference

`scope` decides who can read it afterwards. `work` is shared; a `client-*` scope is that
client's and nobody else's. **Put a client's information in that client's scope.** Writing a
client's detail into `work` leaks it to every agent holding `work`, and nothing downstream
will catch the mistake.

You can only write to a scope your own token already holds. A refusal is the boundary doing
its job — do not retry against a different scope to get around it.

## What happens to what you write

`remember` appends to your own memory file under `<scope>/memory/<agent>/`. It is committed
to git, it is attributed to you, and it is visible to the person who owns the vault.

Claims you write may later be proposed as edits to real pages — and a human accepts, edits,
or rejects each one. **Nothing you write mutates an existing page directly.** Write honestly
and write plainly; someone will read it.
