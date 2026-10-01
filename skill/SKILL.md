---
name: balise
description: Use BEFORE answering any question about how this project or its systems are designed, built, configured, deployed, operated or paid for — architecture, tooling, infrastructure, pipelines, models, data, limits, cost, or any choice already made. A Balise vault is connected over MCP and may hold a recorded decision that overrides the general best-practice answer. Also use when you establish something durable worth recording (propose it, or remember it).
---

# Balise

A Balise vault is connected over MCP. It holds what has already been learned about these
systems — decisions, gotchas, incidents, procedures — as markdown pages carrying short
factual **claims**.

Two habits. Read before you answer. Write when you learn.

## Read first

**Call `context` before answering, even when you already know a good general answer.** That
is the whole point: the vault records what *this* team decided, and a recorded decision
outranks best practice.

A worked example. Asked "should I use `terraform apply -auto-approve` on shared
infrastructure?", the sensible general answer is "not interactively, but it is fine in CI
with guardrails". This vault says something narrower and stricter — never against shared
state, hand the user a saved plan file to run themselves. Answering from general knowledge
is not wrong in the abstract and is still the wrong answer here, and nothing in the question
signals that. Only the vault does.

So the trigger is not "does this sound like it is about their codebase". It is: could this
team have an opinion on it? For infrastructure, tooling, configuration, deployment and
operational practice, assume yes and check.

```
context(query: "why does the apply fail on shared infrastructure", budget_tokens: 6000)
```

Use `context` for questions. It assembles a token-budgeted pack of the most relevant claims
plus the pages they specialize or supersede. Use `search` when you want hits to triage
yourself, `fetch` when you already know the page, `related` to walk from one page to its
neighbours, and `pages` to list a type.

**Read `coverage` before you trust the result.** `coverage: "low"` means the vault did not
have much for that query — say so rather than presenting a thin pack as settled knowledge.
Ranking fuses lexical matching with a semantic signal whenever the vault is configured for
one, so a question phrased in your own words usually still finds the right claims. Where it
is not configured, ranking is lexical only — matching words, not meaning. Either way, if a
first `context` call comes back thin, retry with the vocabulary the systems themselves use
before concluding nothing is there.

If the vault does have the answer, **use it and say where it came from**. That is the
difference between an answer and a guess.

## Write when you learn something

When you establish something durable that the vault does not already hold, offer to record
it. Ask first, one line, not a ceremony, and write on a yes.

> That behaviour isn't in the vault. Want me to record it?

There are two ways to record, and the difference is how sure you are and whether you know
where it belongs.

**`propose`: you verified it and know where it goes.** It puts a proposal in front of the
vault's owner, who accepts, edits or rejects it. First `search` for the page the fact belongs
on. If one is about the same subject, add claims to it:

```
propose(scope: "work", target: "azapi-ergw-connection-deletion",
        claims: ["azapi_update_resource patches the gateway in place and keeps its connections"],
        evidence: "Applied with azapi_update_resource; both connections stayed Connected throughout.")
```

If nothing is about that subject, propose a new page instead, with a type the vault uses
(the refusal lists them if you guess wrong), a hyphenated slug, a title that states the
finding, a short body, and its claims:

```
propose(scope: "client-acme", new_page: {type: "gotcha", slug: "acme-er-gateway-sku",
        title: "ErGw1AZ is rejected in North Central US", body: "..."},
        claims: ["ErGw1AZ is rejected in North Central US despite being listed"], evidence: "...")
```

`evidence` is required: say what you ran, saw or checked. It is the first thing the reviewer
reads. If the page already has a proposal waiting, `propose` refuses; leave it, or `remember`
the fact instead.

**`remember`: an observation in passing, or you are not sure where it belongs.** It appends
to your own memory file and is searchable right away (after the next reindex). The owner
periodically files remembered notes into proposals, so nothing remembered is lost.

```
remember(text: "Concurrent deployment PUTs against one account return 409; serialise them", scope: "work")
```

### What is worth recording

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

`remember` appends to your own memory file under `<scope>/memory/<agent>/`; `propose` writes a
proposal under `review/`. Both are committed to git, attributed to you, and visible to the
person who owns the vault.

A human accepts, edits, or rejects every proposal, whether you wrote it or it was filed from
your notes. **Nothing you write mutates an existing page directly.** Write honestly and write
plainly; someone will read it.
