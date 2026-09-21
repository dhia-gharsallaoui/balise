import { useEffect, useState, type FormEvent } from "react";
import {
  ApiError, createAgent, fetchAgentActivity, fetchAgents, fetchSettings, revokeAgent,
} from "../../lib/api";
import type { AgentActivityRow, AgentState, AgentSummary } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import "../../styles/agents.css";

// The Agents screen (02-ui-design-v1.md section 5.5; layout and tab structure from
// /tmp/balise-design/knowledge-v3.html lines 441-495). It answers exactly two questions a
// person has about something acting on their behalf: what can it read, and what did it just
// do. Both come straight off internal/api/agents.go's two endpoints — nothing here computes a
// number the server did not already hand over.
//
// Add and Revoke became possible over HTTP once commit b0e12a1 gave /api an owner session to
// gate them on (internal/api/agents_write.go); they are real, session-authenticated actions
// here now, not stubs. The Add form's space checkboxes are sourced from a lazily-fetched
// GET /api/settings, not the allScopes prop below — allScopes comes from /api/tree, which
// (internal/api/knowledge.go's handleTree) only lists a space once it holds at least one
// page, so a freshly-declared, still-empty space would never appear in it. settings.go's
// settingsScopes is built from the server's live AllowedScopes() instead, and explicitly
// includes a space the moment it is declared — see that file's own doc comment.
//
// Editing an agent's access in place stays out of scope, on purpose: store.tokens.go has no
// update function at all, by design (02's model is revoke-and-recreate, not mutate-in-place).
// Edit stays a real, visible, disabled tab with an explanatory title — ReviewScreen's "Edit
// then accept (not available yet)" button is the precedent for an honest disabled stub over a
// silently hidden feature.
//
// Copy rule (02 section 8): never say "uid", "hook", "pack", "index", "token", "budget" or
// "trait" on screen, and say "space" where the store/API code says "scope" (see this file's
// own boundarySentence, already following that rule). The Connect tab tooltip is the only
// place a literal CLI-adjacent phrase ("shown once, when an agent is created") appears, and it
// describes behavior, not a command to type.

// The exact three capability strings store.tokens.go's validCapabilities accepts. Hardcoded
// here rather than discovered from the server: there is no endpoint that lists them, and
// inventing a fourth on screen would be exactly the "capability that does not exist" bug the
// brief warns the server-side validator must catch.
const AGENT_CAPABILITIES = ["read", "remember", "propose"] as const;

const MINUTE_MS = 60_000;
const HOUR_MINUTES = 60;
const DAY_HOURS = 24;
const MONTH_DAYS = 30;

// Reimplemented rather than imported from Home.tsx — this codebase's established convention
// (see ReviewScreen/Home, neither of which shares helpers with the other) is a small local
// copy per screen rather than a shared utility module for a handful of one-line formatters.
function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "recently";
  const minutes = Math.round((Date.now() - then) / MINUTE_MS);
  if (minutes < 1) return "just now";
  if (minutes < HOUR_MINUTES) return plural(minutes, "minute") + " ago";
  const hours = Math.round(minutes / HOUR_MINUTES);
  if (hours < DAY_HOURS) return plural(hours, "hour") + " ago";
  const days = Math.round(hours / DAY_HOURS);
  if (days < MONTH_DAYS) return plural(days, "day") + " ago";
  return new Date(iso).toLocaleDateString();
}

function plural(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? "" : "s"}`;
}

function initial(name: string): string {
  const trimmed = name.trim();
  return trimmed ? trimmed[0].toUpperCase() : "?";
}

// joinWithAnd renders a list the way a sentence would say it out loud — "Work", "Work and
// Globex", "Work, Globex, and Personal" — used for both the meta line's scope list and the
// boundary sentence's positive/negative clauses, and (since capabilities is just a short list
// of words like "read"/"remember") for the capability phrase too: joinWithAnd(["read"]) is
// "read", joinWithAnd(["read", "remember"]) is "read and remember" — exactly the two phrases
// 02 section 5.5 names, with no special-casing needed.
function joinWithAnd(items: string[]): string {
  if (items.length === 0) return "";
  if (items.length === 1) return items[0];
  if (items.length === 2) return `${items[0]} and ${items[1]}`;
  return `${items.slice(0, -1).join(", ")}, and ${items[items.length - 1]}`;
}

function capabilityPhrase(capabilities: string[]): string {
  return capabilities.length ? joinWithAnd(capabilities) : "read";
}

function metaLine(agent: AgentSummary): string {
  const reads = agent.scopes.length ? joinWithAnd(agent.scopes) : "nothing";
  return `Reads ${reads} · may ${capabilityPhrase(agent.capabilities)}`;
}

function usedLabel(lastUsedAt?: string | null): string {
  if (!lastUsedAt) return "Never used";
  return `Used ${relativeTime(lastUsedAt)}`;
}

// boundarySentence is 02 section 5.5's headline rule made literal: "The boundary is stated in
// words at the top of each agent: 'This agent can read Work and Globex. It cannot read
// Personal.'" allScopes is optional and defaults to []; when the caller has not supplied the
// vault's full scope list (or every scope this agent can read is already all of them), the
// sentence quietly drops its second half rather than claim an agent "cannot read" a universe
// it was never told about.
function boundarySentence(agent: AgentSummary, allScopes: string[]): string {
  if (agent.state === "revoked") {
    return "This agent's access was revoked. It can no longer read anything.";
  }
  if (agent.state === "expired") {
    return "This agent's access has expired. It can no longer read anything.";
  }
  if (agent.scopes.length === 0) {
    return "This agent cannot read any space.";
  }
  const positive = `This agent can read ${joinWithAnd(agent.scopes)}.`;
  const missing = allScopes.filter((s) => !agent.scopes.includes(s));
  if (missing.length === 0) return positive;
  return `${positive} It cannot read ${joinWithAnd(missing)}.`;
}

function toolVerb(tool: string): string {
  if (tool === "search") return "Looked up";
  if (tool === "remember") return "Remembered";
  return tool.length ? tool[0].toUpperCase() + tool.slice(1) : tool;
}

// resultNote annotates a row with what its result_count actually means for that tool — the
// same number means different things per tool, and conflating them would misdescribe half of
// what it labels:
//
//   - "search" (and, by the same reasoning, "pages"/"context") can legitimately return zero
//     hits — a real, honest "nothing matched" — so it always keeps its plain opened-pages
//     count, zero included, exactly as before.
//   - "fetch" and "related" are different: internal/mcp's fetch.go and related.go only ever
//     record a returned uid on success, so result_count 0 from either one is never a
//     legitimate empty result — it is always a refusal, and 04 section 14 deliberately
//     refuses an unknown slug and an out-of-scope slug identically ("never distinguish
//     'exists elsewhere'"). That refusal is normal, healthy boundary behavior, not an
//     incident, so the note stays as quiet as the existing opened-pages note (same CSS
//     class, no warning color) and never says "denied" — it names the two indistinguishable
//     reasons honestly instead of picking one.
//   - "remember" is left alone: a made-up note under it would read as a failed write rather
//     than what it actually was, matching this screen's existing, deliberate choice not to
//     fabricate one there.
function resultNote(row: AgentActivityRow): string | null {
  if (row.tool === "search") {
    return `Opened ${plural(row.result_count, "page")}`;
  }
  if ((row.tool === "fetch" || row.tool === "related") && row.result_count === 0) {
    return "No matching page — not found, or outside what this agent can read.";
  }
  return null;
}

function StateBadge({ state }: { state: AgentState }) {
  if (state === "active") {
    return <span className="agents-state agents-state-active">active</span>;
  }
  return (
    <span className={`agents-state-badge agents-state-badge-${state}`}>{state}</span>
  );
}

interface AgentsScreenProps {
  allScopes?: string[];
}

type Tab = "activity" | "connect" | "edit";

export function AgentsScreen({ allScopes = [] }: AgentsScreenProps) {
  const [agents, setAgents] = useState<AgentSummary[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("activity");
  const [activity, setActivity] = useState<AgentActivityRow[] | null>(null);
  const [activityError, setActivityError] = useState<string | null>(null);

  // Add-agent form state. availableScopes is fetched lazily, the first time the form opens
  // (see openAddForm's own comment on why /api/settings rather than the allScopes prop) —
  // null means "not fetched yet", [] means "fetched, and the vault has no spaces at all".
  const [showAddForm, setShowAddForm] = useState(false);
  const [addName, setAddName] = useState("");
  const [addExpiry, setAddExpiry] = useState("");
  const [addScopes, setAddScopes] = useState<string[]>([]);
  const [addCapabilities, setAddCapabilities] = useState<string[]>(["read"]);
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);
  const [availableScopes, setAvailableScopes] = useState<string[] | null>(null);
  const [availableScopesError, setAvailableScopesError] = useState<string | null>(null);

  // The one-time token reveal. Cleared the moment the person dismisses it or opens a fresh
  // Add form for another agent — never restored, never re-fetched, because there is nothing
  // to re-fetch it from (see this file's own doc comment and CreateAgentResponse's).
  const [createdToken, setCreatedToken] = useState<{ name: string; token: string } | null>(null);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState<string | null>(null);

  // Revoke confirmation, modeled on ReviewScreen's reject flow (busy/error/confirming trio).
  const [revokeConfirming, setRevokeConfirming] = useState(false);
  const [revokeBusy, setRevokeBusy] = useState(false);
  const [revokeError, setRevokeError] = useState<string | null>(null);

  useEffect(() => {
    fetchAgents()
      .then((res) => {
        setLoadError(null);
        setAgents(res.agents);
        setSelectedId((current) =>
          current && res.agents.some((a) => a.id === current) ? current : (res.agents[0]?.id ?? null),
        );
      })
      .catch((err) => {
        setLoadError(err instanceof Error ? err.message : "Could not load agents.");
      });
  }, []);

  const effectiveId =
    agents && agents.length > 0
      ? (agents.some((a) => a.id === selectedId) ? selectedId : agents[0].id)
      : null;

  // A revoke confirmation for one agent must never carry over onto the next agent selected —
  // reset it the moment the detail card's subject changes.
  useEffect(() => {
    setRevokeConfirming(false);
    setRevokeBusy(false);
    setRevokeError(null);
  }, [effectiveId]);

  useEffect(() => {
    if (!effectiveId) {
      setActivity(null);
      setActivityError(null);
      return;
    }
    let cancelled = false;
    setTab("activity");
    setActivity(null);
    setActivityError(null);
    fetchAgentActivity(effectiveId)
      .then((res) => {
        if (!cancelled) setActivity(res.activity);
      })
      .catch((err) => {
        if (!cancelled) {
          setActivityError(err instanceof Error ? err.message : "Could not load this agent's activity.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [effectiveId]);

  if (loadError) {
    return (
      <EmptyState
        title="Could not reach the server."
        hint={`${loadError}. Is balise serve running?`}
      />
    );
  }

  if (!agents) return null;

  const selected = agents.find((a) => a.id === effectiveId) ?? null;

  // openAddForm fetches /api/settings, not this component's allScopes prop: allScopes comes
  // from /api/tree, which only lists a space once it holds a page (internal/api/knowledge.go's
  // handleTree), so a freshly-declared, still-empty space would never appear as a checkbox.
  // /api/settings's scopes field is built from the server's live AllowedScopes() and lists a
  // space the moment it exists — see this file's top-of-file doc comment. Fetched lazily
  // (only when the form actually opens) rather than on mount, so a screen that never opens
  // the form makes exactly the same two requests it always has.
  function openAddForm() {
    setShowAddForm(true);
    setAddError(null);
    if (!availableScopes && !availableScopesError) {
      fetchSettings()
        .then((res) => setAvailableScopes(res.scopes.map((s) => s.name)))
        .catch((err) => {
          setAvailableScopesError(
            err instanceof Error ? err.message : "Could not load the list of spaces.",
          );
        });
    }
  }

  function closeAddForm() {
    setShowAddForm(false);
    setAddName("");
    setAddExpiry("");
    setAddScopes([]);
    setAddCapabilities(["read"]);
    setAddError(null);
  }

  function toggleScope(scope: string) {
    setAddScopes((current) =>
      current.includes(scope) ? current.filter((s) => s !== scope) : [...current, scope],
    );
  }

  function toggleCapability(capability: string) {
    setAddCapabilities((current) =>
      current.includes(capability)
        ? current.filter((c) => c !== capability)
        : [...current, capability],
    );
  }

  // The server is the real validator (space names must already exist, capabilities must be
  // one of read/remember/propose, no duplicate still-live name) — these two client-side
  // checks exist only to avoid a pointless round trip for the two mistakes a person is most
  // likely to make, never as a substitute for handleCreateAgent's own checks.
  async function handleAddSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = addName.trim();
    if (!name) {
      setAddError("Name the agent before adding it.");
      return;
    }
    if (addScopes.length === 0) {
      setAddError("Choose at least one space this agent may read.");
      return;
    }
    setAddBusy(true);
    setAddError(null);
    try {
      const res = await createAgent({
        name,
        scopes: addScopes,
        capabilities: addCapabilities.length ? addCapabilities : undefined,
        expires_at: addExpiry ? `${addExpiry}T00:00:00Z` : undefined,
      });
      setAgents((current) => (current ? [...current, res.agent] : [res.agent]));
      setSelectedId(res.agent.id);
      setCreatedToken({ name: res.agent.name, token: res.token });
      setCopied(false);
      setCopyError(null);
      closeAddForm();
    } catch (err) {
      setAddError(err instanceof ApiError ? err.message : "Could not add this agent. Try again.");
    } finally {
      setAddBusy(false);
    }
  }

  function handleDismissToken() {
    setCreatedToken(null);
    setCopied(false);
    setCopyError(null);
  }

  // navigator.clipboard is guarded rather than assumed: it can be absent in a real,
  // insecure-context browser (no getUserMedia-style HTTPS/localhost origin), and older or
  // stripped-down test environments may not implement it either. Either way, the fallback
  // is an honest instruction, never a silently swallowed failure.
  async function handleCopyToken() {
    if (!createdToken) return;
    try {
      if (!navigator.clipboard?.writeText) throw new Error("no clipboard API available");
      await navigator.clipboard.writeText(createdToken.token);
      setCopied(true);
      setCopyError(null);
    } catch {
      setCopied(false);
      setCopyError("Could not copy automatically — select and copy the text above by hand.");
    }
  }

  async function handleConfirmRevoke() {
    if (!selected) return;
    setRevokeBusy(true);
    setRevokeError(null);
    try {
      const res = await revokeAgent(selected.id);
      setAgents((current) =>
        current ? current.map((a) => (a.id === res.agent.id ? res.agent : a)) : current,
      );
      setRevokeConfirming(false);
    } catch (err) {
      setRevokeError(
        err instanceof ApiError ? err.message : "Could not revoke this agent. Try again.",
      );
    } finally {
      setRevokeBusy(false);
    }
  }

  return (
    <div className="agents-screen">
      <section className="agents-list-card" aria-labelledby="agents-list-heading">
        <div className="agents-list-head">
          <h2 id="agents-list-heading">Agents</h2>
          <span className="agents-list-count">{agents.length}</span>
          <button
            type="button"
            className="agents-add-toggle"
            aria-expanded={showAddForm}
            onClick={() => (showAddForm ? closeAddForm() : openAddForm())}
          >
            {showAddForm ? "Cancel" : "Add agent"}
          </button>
        </div>

        {showAddForm ? (
          <form className="agents-add-form" onSubmit={handleAddSubmit}>
            <div className="agents-form-field">
              <label htmlFor="agents-add-name">Name</label>
              <input
                id="agents-add-name"
                type="text"
                value={addName}
                onChange={(e) => setAddName(e.target.value)}
                maxLength={100}
              />
            </div>

            <fieldset className="agents-form-field">
              <legend>Spaces it may read</legend>
              {availableScopesError ? (
                <p className="agents-form-error" role="alert">
                  {availableScopesError}
                </p>
              ) : !availableScopes ? (
                <p className="agents-empty-note">Loading spaces…</p>
              ) : availableScopes.length === 0 ? (
                <p className="agents-empty-note">
                  No spaces exist yet — add one from Settings first.
                </p>
              ) : (
                <div className="agents-checkbox-group">
                  {availableScopes.map((scope) => (
                    <label key={scope} className="agents-checkbox">
                      <input
                        type="checkbox"
                        checked={addScopes.includes(scope)}
                        onChange={() => toggleScope(scope)}
                      />
                      {scope}
                    </label>
                  ))}
                </div>
              )}
            </fieldset>

            <fieldset className="agents-form-field">
              <legend>What it may do</legend>
              <div className="agents-checkbox-group">
                {AGENT_CAPABILITIES.map((capability) => (
                  <label key={capability} className="agents-checkbox">
                    <input
                      type="checkbox"
                      checked={addCapabilities.includes(capability)}
                      onChange={() => toggleCapability(capability)}
                    />
                    {capability}
                  </label>
                ))}
              </div>
            </fieldset>

            <div className="agents-form-field">
              <label htmlFor="agents-add-expiry">Expires on (optional)</label>
              <input
                id="agents-add-expiry"
                type="date"
                value={addExpiry}
                onChange={(e) => setAddExpiry(e.target.value)}
              />
            </div>

            {addError ? (
              <p className="agents-form-error" role="alert">
                {addError}
              </p>
            ) : null}

            <div className="agents-form-actions">
              <button type="submit" className="agents-form-submit" disabled={addBusy}>
                {addBusy ? "Adding…" : "Add agent"}
              </button>
              <button type="button" className="agents-form-cancel" onClick={closeAddForm}>
                Cancel
              </button>
            </div>
          </form>
        ) : null}

        {createdToken ? (
          <div className="agents-token-panel">
            <p className="agents-token-warning" role="alert">
              This is the only time {createdToken.name}&rsquo;s access key is shown. Copy it now
              — it cannot be displayed again.
            </p>
            <code className="agents-token-value">{createdToken.token}</code>
            {copyError ? <p className="agents-form-error">{copyError}</p> : null}
            <div className="agents-token-actions">
              <button type="button" className="agents-token-copy" onClick={handleCopyToken}>
                {copied ? "Copied" : "Copy"}
              </button>
              <button type="button" className="agents-token-done" onClick={handleDismissToken}>
                Done
              </button>
            </div>
          </div>
        ) : null}

        {agents.length === 0 ? (
          <p className="agents-empty-note">
            No agents have been given access to this vault yet. Add one above to get started.
          </p>
        ) : (
          <ul className="agents-list">
            {agents.map((agent) => (
              <li key={agent.id}>
                <button
                  type="button"
                  className={`agents-row${agent.id === effectiveId ? " is-selected" : ""}`}
                  onClick={() => setSelectedId(agent.id)}
                  aria-current={agent.id === effectiveId}
                >
                  <span className="agents-avatar" aria-hidden="true">
                    {initial(agent.name)}
                  </span>
                  <span className="agents-row-body">
                    <span className="agents-row-name">{agent.name}</span>
                    <span className="agents-row-meta">{metaLine(agent)}</span>
                  </span>
                  <span className="agents-row-side">
                    <StateBadge state={agent.state} />
                    <span className="agents-row-used">{usedLabel(agent.last_used_at)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
        <p className="agents-list-foot">
          An agent only ever sees the spaces listed next to its name. Add one above, or open an
          agent's details to revoke it.
        </p>
      </section>

      <section className="agents-detail-card" aria-labelledby="agents-detail-heading">
        {selected ? (
          <>
            <div className="agents-detail-head">
              <h2 id="agents-detail-heading" className="agents-detail-name">
                {selected.name}
              </h2>
              <StateBadge state={selected.state} />
              <p className="agents-boundary">{boundarySentence(selected, allScopes)}</p>

              {selected.state !== "revoked" ? (
                revokeConfirming ? (
                  <div className="agents-revoke-panel">
                    <p>
                      Revoke {selected.name}? It will stop reading anything immediately. Its
                      history stays, but this cannot be undone.
                    </p>
                    {revokeError ? (
                      <p className="agents-action-error" role="alert">
                        {revokeError}
                      </p>
                    ) : null}
                    <div className="agents-revoke-actions">
                      <button
                        type="button"
                        className="agents-revoke-confirm"
                        onClick={handleConfirmRevoke}
                        disabled={revokeBusy}
                      >
                        {revokeBusy ? "Revoking…" : "Yes, revoke it"}
                      </button>
                      <button
                        type="button"
                        className="agents-revoke-cancel"
                        onClick={() => setRevokeConfirming(false)}
                        disabled={revokeBusy}
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    className="agents-revoke-button"
                    onClick={() => setRevokeConfirming(true)}
                  >
                    Revoke
                  </button>
                )
              ) : null}
            </div>

            <div className="agents-tabs" role="tablist" aria-label="Agent details">
              <button
                type="button"
                role="tab"
                aria-selected={tab === "activity"}
                className={`agents-tab${tab === "activity" ? " is-active" : ""}`}
                onClick={() => setTab("activity")}
              >
                Activity
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={false}
                className="agents-tab"
                disabled
                title="Connection details are shown once, when an agent is created, and cannot be re-displayed here."
              >
                Connect
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={false}
                className="agents-tab"
                disabled
                title="An agent's access cannot be changed in place. Revoke it and add a new one with the access it needs."
              >
                Edit
              </button>
            </div>

            <div className="agents-tab-panel">
              {activityError ? (
                <p className="agents-empty-note">{activityError}</p>
              ) : !activity ? (
                <p className="agents-empty-note">Loading…</p>
              ) : activity.length === 0 ? (
                <p className="agents-empty-note">This agent has not looked up anything yet.</p>
              ) : (
                <ul className="agents-activity-list">
                  {activity.map((row) => (
                    <li key={row.id} className="agents-activity-row">
                      <span className="agents-activity-when">{relativeTime(row.when)}</span>
                      <div className="agents-activity-body">
                        <div className="agents-activity-query">
                          {toolVerb(row.tool)}: {row.query}
                        </div>
                        {resultNote(row) ? (
                          <div className="agents-activity-note">{resultNote(row)}</div>
                        ) : null}
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </>
        ) : (
          <p className="agents-empty-note">Select an agent to see what it can read.</p>
        )}
      </section>
    </div>
  );
}
