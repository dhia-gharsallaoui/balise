import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { ArrowSquareOut, CaretLeft, Warning } from "@phosphor-icons/react";
import { acceptReview, editAcceptReview, fetchReview, fetchReviewItem, rejectReview } from "../../lib/api";
import type { ReviewClaim, ReviewClaimEdit, ReviewDetail, ReviewListItem, ReviewNewPage } from "../../lib/types";
import { plural } from "../../lib/plural";
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
// A queue row names the proposal kind (a small neutral label), the target title and where it
// came from, not the spec example's claim count ("3 claims on ...") — /api/review's list
// endpoint does not return a per-item count, and fetching every row's detail just to count
// claims would turn one list load into N detail fetches. /api/review/{id} (the right column)
// does show the real before/after claims, so the count is one click away rather than absent.

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

function kindWord(kind: string): string {
  return KIND_LABELS[kind] ?? kind;
}

function kindLabel(item: ReviewListItem): string {
  return kindWord(item.kind);
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
  display?: (value: string) => string;
}

// Hidden entirely when there is only one (or zero) option: a filter with nothing to filter
// between is not a real control, just noise above the list.
function ChipRow({ label, options, active, onSelect, display = (v) => v }: ChipRowProps) {
  if (options.length < 2) return null;
  return (
    <div className="segmented review-filter" role="group" aria-label={label}>
      <button type="button" aria-pressed={active === null} onClick={() => onSelect(null)}>
        All
      </button>
      {options.map((opt) => (
        <button key={opt} type="button" aria-pressed={active === opt} onClick={() => onSelect(opt)}>
          {display(opt)}
        </button>
      ))}
    </div>
  );
}

function KindLabel({ children }: { children: string }) {
  return <span className="review-kind">{children}</span>;
}

interface QueueRowProps {
  item: ReviewListItem;
  selected: boolean;
  onSelect: (id: string) => void;
}

function QueueRow({ item, selected, onSelect }: QueueRowProps) {
  return (
    <li>
      <button
        type="button"
        className={`review-queue-row${selected ? " is-selected" : ""}`}
        onClick={() => onSelect(item.id)}
        aria-current={selected}
      >
        <span className="review-queue-top">
          <KindLabel>{kindLabel(item)}</KindLabel>
          <span className={`review-confidence review-confidence-${item.confidence}`}>{item.confidence}</span>
        </span>
        <span className="review-queue-what">
          <RichTitle title={item.target_title} />
        </span>
        <span className="review-queue-meta">
          <span className="review-queue-scope">{item.scope}</span>
          <span>{sourceWord(item.created_by)}</span>
        </span>
      </button>
    </li>
  );
}

// A reworded claim carries only its new wording; the old wording is the "before" claim with
// the same id. Showing both, old struck through above new, is what makes the change legible
// without the reader diffing the two columns by eye.
function ClaimList({ title, claims, previous }: { title: string; claims: ReviewClaim[]; previous?: ReviewClaim[] }) {
  return (
    <div className="review-claim-col">
      <h3 className="review-heading">{title}</h3>
      {claims.length === 0 ? (
        <p className="review-muted">No claims.</p>
      ) : (
        <ul className="review-claim-list">
          {claims.map((claim) => {
            const old = claim.mark === "reworded" ? previous?.find((p) => p.id === claim.id) : undefined;
            return (
              <li key={claim.id} className={`review-claim review-claim-${claim.mark}`}>
                {claim.mark !== "unchanged" && <span className="review-mark">{claim.mark}</span>}
                {old && old.text !== claim.text && <s className="review-claim-old">{old.text}</s>}
                <span className="review-claim-text">{claim.text}</span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <p className="review-error" role="alert">
      <Warning size={16} weight="regular" aria-hidden="true" />
      <span>{message}</span>
    </p>
  );
}

interface EvidenceProps {
  detail: ReviewDetail;
  onOpenPage: (scope: string, slug: string, title: string) => void;
}

function EvidenceList({ detail, onOpenPage }: EvidenceProps) {
  return (
    <section className="review-evidence" aria-label="Evidence">
      <h3 className="review-heading">Evidence</h3>
      <ul>
        {detail.evidence.map((ev, idx) => (
          <li key={idx}>
            <blockquote>{ev.text}</blockquote>
            {ev.source.startsWith("agent:") ? (
              <p className="review-evidence-by">Reported by {ev.source.slice("agent:".length)}</p>
            ) : (
              <button
                type="button"
                className="btn btn-quiet btn-sm review-open-page"
                onClick={() => {
                  const cited = citedPage(ev.source);
                  if (ev.source === detail.target || cited.slug === detail.target) {
                    onOpenPage(detail.scope, detail.target, detail.target_title);
                  } else {
                    onOpenPage(cited.scope, cited.slug, cited.slug);
                  }
                }}
              >
                Open {ev.source}
                <ArrowSquareOut size={14} weight="regular" aria-hidden="true" />
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

// The page an evidence citation points at, read from its vault path (<scope>/.../<slug>.md).
// Evidence usually cites the proposal's own target, but a proposal built from remembered notes
// cites the memory page each note came from, which is the page worth opening.
function citedPage(source: string): { scope: string; slug: string } {
  const parts = source.split("/");
  return { scope: parts[0], slug: (parts[parts.length - 1] ?? "").replace(/\.md$/, "") };
}

interface NewPagePreviewProps {
  page: ReviewNewPage;
}

// A new_page proposal has no current page to diff against, so the reviewer sees what accepting
// will create: its type, where it will live, and the body it will carry.
function NewPagePreview({ page }: NewPagePreviewProps) {
  return (
    <section className="review-newpage" aria-label="New page">
      <h3 className="review-heading">New page</h3>
      <dl className="review-newpage-meta">
        <dt>Type</dt>
        <dd>
          <span className="type-dot" data-type={page.type} aria-hidden="true" />
          {page.type}
        </dd>
        <dt>Creates</dt>
        <dd className="review-newpage-path">{page.path}</dd>
      </dl>
      <div className="review-newpage-body">{page.body}</div>
    </section>
  );
}

interface RejectPanelProps {
  reason: string;
  busy: boolean;
  inputRef: RefObject<HTMLInputElement | null>;
  onChange: (value: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
}

function RejectPanel({ reason, busy, inputRef, onChange, onConfirm, onCancel }: RejectPanelProps) {
  return (
    <div className="review-panel review-reject-panel">
      <label htmlFor="review-reject-reason">Reason (one line)</label>
      <input
        id="review-reject-reason"
        className="field"
        ref={inputRef}
        type="text"
        value={reason}
        placeholder="Why this should not land…"
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onConfirm();
          } else if (e.key === "Escape") {
            onCancel();
          }
        }}
      />
      <div className="review-panel-actions">
        <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={busy}>
          Confirm reject
        </button>
        <button type="button" className="btn btn-quiet" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}

function editHint(count: number): string {
  if (count < MIN_CLAIM_WORDS) return "Too short to read as a statement. Add a few more words.";
  if (count > MAX_CLAIM_WORDS) {
    return `${plural(count, "word")}. Longer than the usual limit of 15, but this will still be accepted.`;
  }
  return plural(count, "word");
}

interface EditPanelProps {
  detail: ReviewDetail;
  edits: Record<string, string>;
  busy: boolean;
  firstRef: RefObject<HTMLTextAreaElement | null>;
  onChange: (id: string, value: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
}

function EditPanel({ detail, edits, busy, firstRef, onChange, onConfirm, onCancel }: EditPanelProps) {
  const editable = detail.after.filter(isEditable);
  const hasBlockingEdit = editable.some((claim) => wordCount(edits[claim.id] ?? claim.text) < MIN_CLAIM_WORDS);
  return (
    <div className="review-panel review-edit-panel">
      <p className="review-edit-intro">
        Edit the wording below, then accept. Your edit is recorded as what changed before accept,
        never credited to the agent as its own words.
      </p>
      {editable.map((claim, idx) => {
        const value = edits[claim.id] ?? claim.text;
        const count = wordCount(value);
        const hintId = `review-edit-hint-${claim.id}`;
        const warn = count < MIN_CLAIM_WORDS || count > MAX_CLAIM_WORDS;
        return (
          <div className={`review-edit-field review-claim-${claim.mark}`} key={claim.id}>
            <label htmlFor={`review-edit-${claim.id}`}>
              {claim.mark === "added" ? "New claim" : "Reworded claim"}
            </label>
            <textarea
              id={`review-edit-${claim.id}`}
              className="field"
              ref={idx === 0 ? firstRef : undefined}
              value={value}
              rows={2}
              aria-describedby={hintId}
              aria-invalid={count < MIN_CLAIM_WORDS}
              onChange={(e) => onChange(claim.id, e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") onCancel();
              }}
            />
            <p id={hintId} className={`review-edit-hint${warn ? " is-warning" : ""}`}>
              {warn && <Warning size={14} weight="regular" aria-hidden="true" />}
              {editHint(count)}
            </p>
          </div>
        );
      })}
      <div className="review-panel-actions">
        <button type="button" className="btn btn-primary" onClick={onConfirm} disabled={busy || hasBlockingEdit}>
          Confirm edit and accept
        </button>
        <button type="button" className="btn btn-quiet" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}

interface ActionBarProps {
  busy: boolean;
  canEdit: boolean;
  onAccept: () => void;
  onEdit: () => void;
  onReject: () => void;
}

// Three buttons, always in the same place. The key hints sit beside them as text rather than
// inside the buttons, so each button's accessible name stays exactly its verb.
function ActionBar({ busy, canEdit, onAccept, onEdit, onReject }: ActionBarProps) {
  return (
    <footer className="review-actions">
      <div className="review-actions-buttons">
        <button type="button" className="btn btn-primary review-action" onClick={onAccept} disabled={busy}>
          Accept
        </button>
        <button
          type="button"
          className="btn btn-secondary review-action"
          onClick={onEdit}
          disabled={busy || !canEdit}
          title={
            canEdit
              ? "Edit the wording of an added or reworded claim, then accept."
              : "Nothing to edit: this proposal has no added or reworded claim."
          }
        >
          Edit then accept
        </button>
        <button type="button" className="btn btn-danger review-action" onClick={onReject} disabled={busy}>
          Reject
        </button>
      </div>
      <p className="review-keys" aria-hidden="true">
        <kbd>a</kbd> accept <kbd>e</kbd> edit <kbd>r</kbd> reject <kbd>j</kbd>
        <kbd>k</kbd> move
      </p>
    </footer>
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
  // Phones show one pane at a time: the queue, or the proposal picked from it. Wider screens
  // show both and ignore this; it only drives a data attribute the stylesheet reads.
  const [pane, setPane] = useState<"queue" | "detail">("queue");
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

  function cancelReject() {
    setRejecting(false);
    setReason("");
  }

  function cancelEdit() {
    setEditing(false);
    setEdits({});
  }

  function selectItem(id: string) {
    setSelectedId(id);
    setPane("detail");
  }

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
    <div className="review-screen" data-pane={pane}>
      <aside className="review-queue-col" aria-label="Review queue">
        <div className="review-queue-head">
          <h2 className="review-queue-title">
            Queue <span className="count">{filtered.length}</span>
          </h2>
          <div className="review-filters">
            <ChipRow
              label="Filter by kind"
              options={kinds}
              active={kindFilter}
              onSelect={setKindFilter}
              display={kindWord}
            />
            <ChipRow label="Filter by scope" options={scopes} active={scopeFilter} onSelect={setScopeFilter} />
          </div>
        </div>
        {filtered.length === 0 ? (
          <p className="review-muted review-queue-empty">No proposals match this filter.</p>
        ) : (
          <ul className="review-queue-list">
            {filtered.map((item) => (
              <QueueRow key={item.id} item={item} selected={item.id === effectiveId} onSelect={selectItem} />
            ))}
          </ul>
        )}
      </aside>

      <section className="review-detail-col" aria-label="Proposal">
        <button type="button" className="btn btn-quiet btn-sm review-back" onClick={() => setPane("queue")}>
          <CaretLeft size={14} weight="regular" aria-hidden="true" />
          All proposals
        </button>
        {detailError ? (
          <EmptyState title="Could not load this proposal." hint={detailError} />
        ) : !detail || detail.id !== effectiveId ? (
          <p className="review-muted">Loading…</p>
        ) : (
          <>
            <header className="review-detail-header">
              <p className="review-detail-meta">
                <KindLabel>{kindLabel(detail)}</KindLabel>
                <span>Confidence: {detail.confidence}</span>
                <span>{sourceWord(detail.created_by)}</span>
              </p>
              <h2 className="review-detail-title">
                <RichTitle title={detail.target_title} />
              </h2>
              <p className="review-detail-path">
                {detail.new_page ? detail.new_page.path : detail.target} · {detail.scope}
              </p>
            </header>

            <EvidenceList detail={detail} onOpenPage={onOpenPage} />

            {detail.new_page ? (
              <>
                <NewPagePreview page={detail.new_page} />
                <section className="review-claims review-claims-single" aria-label="Claims">
                  <ClaimList title="Claims" claims={detail.after} previous={detail.before} />
                </section>
              </>
            ) : (
              <section className="review-claims" aria-label="Claims">
                <ClaimList title="Current" claims={detail.before} />
                <ClaimList title="Proposed" claims={detail.after} previous={detail.before} />
              </section>
            )}

            {actionError && <ErrorBanner message={actionError} />}

            {rejecting && (
              <RejectPanel
                reason={reason}
                busy={busy}
                inputRef={reasonRef}
                onChange={setReason}
                onConfirm={() => void handleReject(detail.id)}
                onCancel={cancelReject}
              />
            )}

            {editing && (
              <EditPanel
                detail={detail}
                edits={edits}
                busy={busy}
                firstRef={firstEditRef}
                onChange={(id, value) => setEdits((prev) => ({ ...prev, [id]: value }))}
                onConfirm={() => void handleEditAccept(detail.id)}
                onCancel={cancelEdit}
              />
            )}

            {/* While a reject reason or an edit is open, that panel's own confirm is the one
                primary action on screen; the standing three would compete with it. */}
            {!rejecting && !editing && (
              <ActionBar
                busy={busy}
                canEdit={detail.after.some(isEditable)}
                onAccept={() => void handleAccept(detail.id)}
                onEdit={startEditing}
                onReject={() => setRejecting(true)}
              />
            )}
          </>
        )}
      </section>
    </div>
  );
}
