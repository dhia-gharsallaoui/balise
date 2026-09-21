import { useEffect, useMemo, useRef, useState } from "react";
import { acceptReview, editAcceptReview, fetchReview, fetchReviewItem, rejectReview } from "../../lib/api";
import type { ReviewClaim, ReviewClaimEdit, ReviewDetail, ReviewListItem } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { RichTitle } from "../ui/RichTitle";
import "../../styles/review.css";

// Review screen (02-ui-design-v1.md section 5.3): two columns, evidence first, three buttons
// always in the same place, keyboard a/e/r/j/k, next item loads automatically on action.
//
// "Edit then accept" (04-technical-spec-v1.md section 13's POST .../edit-accept) lets a
// reviewer retype the wording of a claim the proposal is about to add or reword, before it is
// committed. Only "added" and "reworded" claims are editable — a "kept" or "retired" claim is
// the page's own untouched history, not part of "the change" the spec means. An edit never
// changes who wrote it: the commit still lands as the proposing agent (provenance is honest
// about origin), but its message names exactly which claim a human retyped, so the wording is
// never silently credited to the machine. The same word-count rule that gates a freshly
// proposed claim (2-15 words; over 15 warns, it never blocks) applies to an edited one too —
// shown here live, as text, not only as a colour change.
//
// The queue row's "what changes" text is `<kind> on <target title>` rather than the spec
// example's claim count ("3 claims on ...") — /api/review's list endpoint does not return a
// per-item count, and fetching every row's detail just to count claims would turn one list
// load into N detail fetches. /api/review/{id} (the right column) does show the real
// before/after claims, so the count is one click away rather than absent.

const MIN_CLAIM_WORDS = 2;
const MAX_CLAIM_WORDS = 15;

function wordCount(text: string): number {
  const trimmed = text.trim();
  return trimmed ? trimmed.split(/\s+/).length : 0;
}

function isEditable(claim: ReviewClaim): boolean {
  return claim.mark === "added" || claim.mark === "reworded";
}

interface ReviewScreenProps {
  onOpenPage: (scope: string, slug: string, title: string) => void;
}

const KIND_LABELS: Record<string, string> = {
  claims: "Claims",
  new_page: "New page",
  update: "Update",
  relation: "Relation",
  merge: "Merge",
  classify: "Reclassify",
  split: "Split",
};

function kindLabel(item: ReviewListItem): string {
  return KIND_LABELS[item.kind] ?? item.kind;
}

function sourceWord(createdBy: string): string {
  const idx = createdBy.indexOf(":");
  if (idx < 0) return `from ${createdBy}`;
  const pipeline = createdBy.slice(0, idx);
  const date = new Date(createdBy.slice(idx + 1));
  if (Number.isNaN(date.getTime())) return `from ${pipeline}`;
  const formatted = date.toLocaleDateString(undefined, { day: "numeric", month: "short" });
  return `from ${pipeline}, ${formatted}`;
}

function uniqueValues<T>(items: T[], keyFn: (item: T) => string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of items) {
    const key = keyFn(item);
    if (!seen.has(key)) {
      seen.add(key);
      out.push(key);
    }
  }
  return out;
}

interface ChipRowProps {
  label: string;
  options: string[];
  active: string | null;
  onSelect: (value: string | null) => void;
}

// Hidden entirely when there is only one (or zero) option: a filter with nothing to filter
// between is not a real control, just noise above the list.
function ChipRow({ label, options, active, onSelect }: ChipRowProps) {
  if (options.length < 2) return null;
  return (
    <div className="review-chip-row" role="group" aria-label={label}>
      <button type="button" className="review-chip" aria-pressed={active === null} onClick={() => onSelect(null)}>
        all
      </button>
      {options.map((opt) => (
        <button
          key={opt}
          type="button"
          className="review-chip"
          aria-pressed={active === opt}
          onClick={() => onSelect(opt)}
        >
          {opt}
        </button>
      ))}
    </div>
  );
}

function ClaimList({ title, claims }: { title: string; claims: ReviewClaim[] }) {
  return (
    <div className="review-claim-col">
      <h4>{title}</h4>
      {claims.length === 0 ? (
        <p className="review-claim-empty">No claims.</p>
      ) : (
        <ul className="review-claim-list">
          {claims.map((claim) => (
            <li key={claim.id} className="review-claim">
              {claim.mark !== "unchanged" && (
                <span className={`review-mark review-mark-${claim.mark}`}>{claim.mark}</span>
              )}
              <span className="review-claim-text">{claim.text}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function ReviewScreen({ onOpenPage }: ReviewScreenProps) {
  const [items, setItems] = useState<ReviewListItem[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReviewDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState("");
  const [editing, setEditing] = useState(false);
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [kindFilter, setKindFilter] = useState<string | null>(null);
  const [scopeFilter, setScopeFilter] = useState<string | null>(null);
  const reasonRef = useRef<HTMLInputElement | null>(null);
  const firstEditRef = useRef<HTMLTextAreaElement | null>(null);

  function loadQueue(preferId?: string | null) {
    fetchReview()
      .then((res) => {
        setItems(res.proposals);
        setLoadError(null);
        setSelectedId((current) => {
          if (preferId && res.proposals.some((p) => p.id === preferId)) return preferId;
          if (current && res.proposals.some((p) => p.id === current)) return current;
          return res.proposals[0]?.id ?? null;
        });
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : "Could not load the review queue."));
  }

  useEffect(() => {
    loadQueue();
    // Only ever runs once, on mount — every later refresh goes through loadQueue directly
    // (accept/reject call it via advance()), not through a re-run of this effect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const filtered = useMemo(() => {
    if (!items) return [];
    return items.filter(
      (it) => (!kindFilter || it.kind === kindFilter) && (!scopeFilter || it.scope === scopeFilter),
    );
  }, [items, kindFilter, scopeFilter]);

  const kinds = useMemo(() => uniqueValues(items ?? [], (it) => it.kind), [items]);
  const scopes = useMemo(() => uniqueValues(items ?? [], (it) => it.scope), [items]);

  const effectiveId = filtered.some((it) => it.id === selectedId) ? selectedId : (filtered[0]?.id ?? null);

  useEffect(() => {
    if (!effectiveId) {
      setDetail(null);
      return;
    }
    let cancelled = false;
    setDetailError(null);
    fetchReviewItem(effectiveId)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((e) => {
        if (!cancelled) setDetailError(e instanceof Error ? e.message : "Could not load this proposal.");
      });
    return () => {
      cancelled = true;
    };
  }, [effectiveId]);

  // Neither in-progress panel survives a move to a different item — a half-typed rejection
  // reason or claim edit for p-1 must never leak onto p-2's screen.
  useEffect(() => {
    setRejecting(false);
    setReason("");
    setEditing(false);
    setEdits({});
  }, [effectiveId]);

  useEffect(() => {
    if (rejecting) reasonRef.current?.focus();
  }, [rejecting]);

  useEffect(() => {
    if (editing) firstEditRef.current?.focus();
  }, [editing]);

  function startEditing() {
    if (!detail || !detail.after.some(isEditable)) return;
    const initial: Record<string, string> = {};
    for (const claim of detail.after) {
      if (isEditable(claim)) initial[claim.id] = claim.text;
    }
    setEdits(initial);
    setEditing(true);
    setRejecting(false);
    setReason("");
  }

  function advance(fromId: string) {
    const idx = filtered.findIndex((it) => it.id === fromId);
    const next = filtered[idx + 1] ?? filtered[idx - 1] ?? null;
    loadQueue(next?.id ?? null);
  }

  function moveSelection(delta: number) {
    if (!effectiveId) return;
    const idx = filtered.findIndex((it) => it.id === effectiveId);
    const next = filtered[idx + delta];
    if (next) setSelectedId(next.id);
  }

  async function handleAccept(id: string) {
    setBusy(true);
    setActionError(null);
    try {
      await acceptReview(id);
      setRejecting(false);
      setReason("");
      advance(id);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Accept failed.");
    } finally {
      setBusy(false);
    }
  }

  async function handleReject(id: string) {
    const trimmed = reason.trim();
    if (!trimmed || trimmed.includes("\n")) {
      setActionError("Reason must be a single non-empty line.");
      return;
    }
    setBusy(true);
    setActionError(null);
    try {
      await rejectReview(id, trimmed);
      setRejecting(false);
      setReason("");
      advance(id);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Reject failed.");
    } finally {
      setBusy(false);
    }
  }

  // Only a claim whose text a reviewer actually changed travels to the server — a claim opened
  // for editing but left untouched must not turn into an "edited before accept" commit note it
  // does not deserve.
  async function handleEditAccept(id: string) {
    if (!detail) return;
    const changed: ReviewClaimEdit[] = [];
    for (const claim of detail.after) {
      if (!isEditable(claim)) continue;
      const value = (edits[claim.id] ?? claim.text).trim();
      if (value !== claim.text.trim()) changed.push({ id: claim.id, text: value });
    }
    setBusy(true);
    setActionError(null);
    try {
      await editAcceptReview(id, changed);
      setEditing(false);
      setEdits({});
      advance(id);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Edit then accept failed.");
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    function isTyping(target: EventTarget | null) {
      const el = target as HTMLElement | null;
      return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA");
    }
    function onKeyDown(ev: KeyboardEvent) {
      if (isTyping(ev.target)) {
        if (ev.key === "Escape") {
          setRejecting(false);
          setReason("");
          setEditing(false);
          setEdits({});
        }
        return;
      }
      if (!effectiveId) return;
      if (ev.key === "a") void handleAccept(effectiveId);
      else if (ev.key === "e") startEditing();
      else if (ev.key === "r") setRejecting(true);
      else if (ev.key === "j") moveSelection(1);
      else if (ev.key === "k") moveSelection(-1);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
    // startEditing closes over `detail`, so it must be current whenever "e" fires — including
    // this effect in the dependency list (via `detail`) keeps the listener's copy fresh rather
    // than pinned to whatever proposal was loaded when the item was first selected.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveId, filtered, detail]);

  if (loadError) {
    return (
      <EmptyState title="Could not load the review queue." hint={`${loadError}. Is balise serve running?`} />
    );
  }
  if (items && items.length === 0) {
    return (
      <EmptyState
        title="Nothing is waiting for review."
        hint="New proposals appear here once a compile run produces them."
      />
    );
  }
  if (!items) return null;

  return (
    <div className="review-screen">
      <aside className="review-queue-col">
        <div className="review-filters">
          <ChipRow label="Filter by kind" options={kinds} active={kindFilter} onSelect={setKindFilter} />
          <ChipRow label="Filter by scope" options={scopes} active={scopeFilter} onSelect={setScopeFilter} />
        </div>
        {filtered.length === 0 ? (
          <p className="review-claim-empty">No proposals match this filter.</p>
        ) : (
          <ul className="review-queue-list">
            {filtered.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  className={`review-queue-row${item.id === effectiveId ? " is-selected" : ""}`}
                  onClick={() => setSelectedId(item.id)}
                  aria-current={item.id === effectiveId}
                >
                  <span className="review-queue-what">
                    {kindLabel(item)} on <RichTitle title={item.target_title} />
                  </span>
                  <span className="review-queue-meta">
                    <span>{sourceWord(item.created_by)}</span>
                    <span className={`review-confidence review-confidence-${item.confidence}`}>
                      {item.confidence}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </aside>

      <section className="review-detail-col">
        {detailError ? (
          <EmptyState title="Could not load this proposal." hint={detailError} />
        ) : !detail || detail.id !== effectiveId ? (
          <p className="review-claim-empty">Loading…</p>
        ) : (
          <>
            <header className="review-detail-header">
              <h3><RichTitle title={detail.target_title} /></h3>
              <p className="review-detail-sub">
                {detail.kind} on {detail.target} · {detail.scope}
              </p>
            </header>

            <section className="review-evidence" aria-label="Evidence">
              <h4>Evidence</h4>
              <ul>
                {detail.evidence.map((ev, idx) => (
                  <li key={idx}>
                    <blockquote>{ev.text}</blockquote>
                    <button
                      type="button"
                      className="review-open-page"
                      onClick={() => onOpenPage(detail.scope, detail.target, detail.target_title)}
                    >
                      Open {ev.source}
                    </button>
                  </li>
                ))}
              </ul>
            </section>

            <section className="review-claims" aria-label="Claims">
              <ClaimList title="Current" claims={detail.before} />
              <ClaimList title="Proposed" claims={detail.after} />
            </section>

            {actionError && (
              <p className="review-action-error" role="alert">
                {actionError}
              </p>
            )}

            {rejecting && (
              <div className="review-reject-panel">
                <label htmlFor="review-reject-reason">Reason (one line)</label>
                <input
                  id="review-reject-reason"
                  ref={reasonRef}
                  type="text"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      void handleReject(detail.id);
                    } else if (e.key === "Escape") {
                      setRejecting(false);
                      setReason("");
                    }
                  }}
                />
                <div className="review-reject-actions">
                  <button type="button" onClick={() => void handleReject(detail.id)} disabled={busy}>
                    Confirm reject
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setRejecting(false);
                      setReason("");
                    }}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}

            {editing && (() => {
              const editable = detail.after.filter(isEditable);
              const hasBlockingEdit = editable.some(
                (claim) => wordCount(edits[claim.id] ?? claim.text) < MIN_CLAIM_WORDS,
              );
              return (
                <div className="review-edit-panel">
                  <p className="review-edit-intro">
                    Edit the wording below, then accept. Your edit is recorded as what changed
                    before accept — it is never credited to the agent as its own words.
                  </p>
                  {editable.map((claim, idx) => {
                    const value = edits[claim.id] ?? claim.text;
                    const count = wordCount(value);
                    const tooShort = count < MIN_CLAIM_WORDS;
                    const tooLong = count > MAX_CLAIM_WORDS;
                    const hintId = `review-edit-hint-${claim.id}`;
                    return (
                      <div className="review-edit-field" key={claim.id}>
                        <label htmlFor={`review-edit-${claim.id}`}>
                          {claim.mark === "added" ? "New claim" : "Reworded claim"}
                        </label>
                        <textarea
                          id={`review-edit-${claim.id}`}
                          ref={idx === 0 ? firstEditRef : undefined}
                          value={value}
                          rows={2}
                          aria-describedby={hintId}
                          aria-invalid={tooShort}
                          onChange={(e) =>
                            setEdits((prev) => ({ ...prev, [claim.id]: e.target.value }))
                          }
                          onKeyDown={(e) => {
                            if (e.key === "Escape") {
                              setEditing(false);
                              setEdits({});
                            }
                          }}
                        />
                        <p
                          id={hintId}
                          className={`review-edit-hint${tooShort || tooLong ? " is-warning" : ""}`}
                        >
                          {tooShort
                            ? "Too short to read as a statement — add a few more words."
                            : tooLong
                              ? `${count} words — longer than the usual limit of 15, but this will still be accepted.`
                              : `${count} words`}
                        </p>
                      </div>
                    );
                  })}
                  <div className="review-edit-actions">
                    <button
                      type="button"
                      onClick={() => void handleEditAccept(detail.id)}
                      disabled={busy || hasBlockingEdit}
                    >
                      Confirm edit and accept
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setEditing(false);
                        setEdits({});
                      }}
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              );
            })()}

            <footer className="review-actions">
              <button
                type="button"
                className="review-action review-action-accept"
                onClick={() => void handleAccept(detail.id)}
                disabled={busy}
              >
                Accept
              </button>
              <button
                type="button"
                className="review-action review-action-edit"
                onClick={startEditing}
                disabled={busy || !detail.after.some(isEditable)}
                title={
                  detail.after.some(isEditable)
                    ? "Edit the wording of an added or reworded claim, then accept."
                    : "Nothing to edit — this proposal has no added or reworded claim."
                }
              >
                Edit then accept
              </button>
              <button
                type="button"
                className="review-action review-action-reject"
                onClick={() => setRejecting(true)}
                disabled={busy}
              >
                Reject
              </button>
            </footer>
          </>
        )}
      </section>
    </div>
  );
}
