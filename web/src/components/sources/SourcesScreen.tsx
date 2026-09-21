import { useEffect, useState, type ChangeEvent, type DragEvent, type FormEvent } from "react";
import { plural } from "../../lib/plural";
import { ApiError, fetchSourcesLog, uploadSourcePage } from "../../lib/api";
import type { HomeChange, HomePageRef } from "../../lib/types";
import { RichTitle } from "../ui/RichTitle";
import "../../styles/sources.css";

// The Sources screen (AppShell's subtitle for this section: "Where knowledge comes in"; markup
// spec at /tmp/balise-design/knowledge-v3.html lines 397-440). Three cards, in the mockup's
// order:
//
//   1. Connected sources — there is no connector framework in this build (no connectors table,
//      no sync jobs), so this is a static, honest empty state rather than the fabricated
//      dot/name/"feeds N"/action rows v3's markup shows. It says only what is true right now,
//      and never implies a connector framework is coming in a later release.
//   2. Drop files here or paste text — the one real feature. A person drops a markdown file or
//      pastes text, picks a space, and it becomes a page committed to the vault
//      (POST /api/sources/pages, internal/api/sources.go). v3's original copy promised PDFs,
//      images and recordings would be "kept as attachments" — there is no blob store in this
//      build, so that promise is dropped; the copy here says only what the endpoint actually
//      does. The mockup also has no slug field — only a scope <select> — so a URL-safe slug is
//      derived client-side from the title (or the dropped file's name) via slugify() below,
//      matching internal/vault.ValidateSegment's rules (no "/" or "\", never "." or "..", never
//      empty, no control characters): slugify only ever emits [a-z0-9-], so it can never
//      produce a value ValidateSegment would reject.
//   3. Ingest log — real git history (GET /api/sources/log, which is exactly Home's
//      s.homeChanges reused server-side), not a fabricated feed. Same row shape as Home's
//      "Changed recently" card, since both are fed by the identical HomeChange rows.
//
// Copy rule (02 section 8): never say "uid", "index", "token" or "budget" on screen; say
// "page", "space" and "source" instead. Helper functions below (relativeTime, plural,
// dotClassForAuthor) are reimplemented locally rather than imported from Home.tsx — this
// codebase's established convention (see AgentsScreen's identical note) is a small local copy
// per screen, not a shared utility module for a handful of one-line formatters.

const MAX_BYTES = 2 * 1024 * 1024; // mirrors internal/api/sources.go's sourcesMaxBodyBytes
const ACCEPTED_EXTENSIONS = [".md", ".markdown", ".txt"];

interface SourcesScreenProps {
  allScopes?: string[];
  onOpenPage?: (pages: HomePageRef[], label: string) => void;
}

type Feedback =
  | { kind: "idle" }
  | { kind: "error"; message: string }
  | { kind: "success"; scope: string; slug: string; title: string };

export function SourcesScreen({ allScopes = [], onOpenPage }: SourcesScreenProps) {
  const [scope, setScope] = useState(allScopes[0] ?? "");
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [fileName, setFileName] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [feedback, setFeedback] = useState<Feedback>({ kind: "idle" });
  const [isDragOver, setIsDragOver] = useState(false);
  const [log, setLog] = useState<HomeChange[] | null>(null);
  const [logError, setLogError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  // Keeps the space <select> pointed at a real space even if allScopes arrives after first
  // render — AppShell derives it from the page tree, which loads asynchronously.
  useEffect(() => {
    if (!scope && allScopes.length > 0) setScope(allScopes[0]);
  }, [allScopes, scope]);

  useEffect(() => {
    let cancelled = false;
    fetchSourcesLog()
      .then((res) => {
        if (!cancelled) setLog(res.ingests);
      })
      .catch((err) => {
        if (!cancelled) setLogError(err instanceof Error ? err.message : "Could not load the ingest log.");
      });
    return () => {
      cancelled = true;
    };
  }, [refreshKey]);

  function acceptFile(file: File) {
    const lowerName = file.name.toLowerCase();
    const looksLikeText =
      ACCEPTED_EXTENSIONS.some((ext) => lowerName.endsWith(ext)) ||
      file.type === "text/markdown" ||
      file.type === "text/plain" ||
      file.type === "";
    if (!looksLikeText) {
      setFeedback({ kind: "error", message: "Only markdown and plain text files are supported." });
      return;
    }
    if (file.size > MAX_BYTES) {
      setFeedback({ kind: "error", message: "That file is larger than the 2 MB limit for a single drop." });
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      setContent(String(reader.result ?? ""));
      setFileName(file.name);
      setFeedback({ kind: "idle" });
    };
    reader.onerror = () => {
      setFeedback({ kind: "error", message: "Could not read that file." });
    };
    reader.readAsText(file);
  }

  function handleFileChange(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (file) acceptFile(file);
    e.target.value = ""; // lets the same file be chosen again after fixing an error
  }

  function handleDrop(e: DragEvent<HTMLElement>) {
    e.preventDefault();
    setIsDragOver(false);
    const file = e.dataTransfer.files?.[0];
    if (file) acceptFile(file);
  }

  function handleDragOver(e: DragEvent<HTMLElement>) {
    e.preventDefault();
    setIsDragOver(true);
  }

  function handleDragLeave() {
    setIsDragOver(false);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!scope) {
      setFeedback({ kind: "error", message: "Choose a space first." });
      return;
    }
    if (content.trim() === "") {
      setFeedback({ kind: "error", message: "Drop a file or paste some text first." });
      return;
    }
    const nameHint = title || fileNameBase(fileName) || "source";
    const slug = slugify(nameHint);
    setSubmitting(true);
    try {
      const result = await uploadSourcePage({ scope, slug, title: title || undefined, content });
      setFeedback({ kind: "success", scope: result.scope, slug: result.slug, title: result.title });
      setContent("");
      setFileName(null);
      setTitle("");
      setRefreshKey((k) => k + 1);
    } catch (err) {
      setFeedback({ kind: "error", message: describeUploadError(err, scope, nameHint) });
    } finally {
      setSubmitting(false);
    }
  }

  function openIngestedPage(pages: HomePageRef[], label: string) {
    onOpenPage?.(pages, label);
  }

  return (
    <div className="sources-page">
      <section className="sources-card" aria-labelledby="sources-connected-heading">
        <div className="sources-card-head">
          <h2 id="sources-connected-heading">Connected sources</h2>
          <span className="sources-card-count">0</span>
        </div>
        <p className="sources-empty">
          No sources are connected. Add a page below by dropping a file or pasting text.
        </p>
      </section>

      <section
        className={`sources-card sources-drop${isDragOver ? " is-dragover" : ""}`}
        aria-labelledby="sources-drop-heading"
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <div className="sources-drop-head">
          <h2 id="sources-drop-heading">Drop files here or paste text</h2>
          <p className="sources-drop-copy">
            Add a source directly: drop a markdown file or paste text below, and it becomes a
            page in the space you choose.
          </p>
        </div>

        <form className="sources-drop-form" onSubmit={handleSubmit}>
          <label className="sources-field" htmlFor="sources-file-input">
            <span>Choose a file</span>
            <input
              id="sources-file-input"
              type="file"
              accept=".md,.markdown,.txt,text/markdown,text/plain"
              onChange={handleFileChange}
            />
          </label>
          <p className="sources-drop-hint">
            {fileName ? `Loaded "${fileName}".` : "or drag a .md, .markdown or .txt file onto this card"}
          </p>

          <label className="sources-field" htmlFor="sources-paste">
            <span>Or paste text</span>
            <textarea
              id="sources-paste"
              value={content}
              onChange={(e) => {
                setContent(e.target.value);
                setFileName(null);
              }}
              placeholder="Paste markdown or plain text here…"
              rows={6}
            />
          </label>

          <div className="sources-drop-row">
            <label className="sources-field sources-field-inline" htmlFor="sources-scope">
              <span>Into</span>
              <select
                id="sources-scope"
                value={scope}
                onChange={(e) => setScope(e.target.value)}
                disabled={allScopes.length === 0}
              >
                {allScopes.length === 0 ? <option value="">No spaces available</option> : null}
                {allScopes.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </label>

            <label className="sources-field sources-field-inline" htmlFor="sources-title">
              <span>Title</span>
              <input
                id="sources-title"
                type="text"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Taken from the text if left blank"
              />
            </label>

            <button type="submit" className="sources-submit" disabled={submitting || !scope}>
              {submitting ? "Adding…" : "Add to vault"}
            </button>
          </div>

          {feedback.kind === "error" ? (
            <p className="sources-feedback sources-feedback-error" role="alert">
              {feedback.message}
            </p>
          ) : feedback.kind === "success" ? (
            <p className="sources-feedback sources-feedback-success" role="status">
              Added &quot;{feedback.title}&quot; to {feedback.scope}.
              {onOpenPage ? (
                <>
                  {" "}
                  <button
                    type="button"
                    className="sources-feedback-open"
                    onClick={() =>
                      openIngestedPage(
                        [{ scope: feedback.scope, slug: feedback.slug, title: feedback.title }],
                        `Added: ${feedback.title}`,
                      )
                    }
                  >
                    Open it
                  </button>
                </>
              ) : null}
            </p>
          ) : null}
        </form>
      </section>

      <section className="sources-card" aria-labelledby="sources-log-heading">
        <div className="sources-card-head">
          <h2 id="sources-log-heading">Ingest log</h2>
          <span className="sources-card-count">{log ? plural(log.length, "entry") : ""}</span>
        </div>

        {logError ? (
          <p className="sources-empty">Could not load the ingest log: {logError}</p>
        ) : log === null ? null : log.length === 0 ? (
          <p className="sources-empty">Nothing has been added yet.</p>
        ) : (
          log.map((change) => <IngestRow key={change.sha} change={change} onOpenPage={openIngestedPage} />)
        )}
      </section>
    </div>
  );
}

interface IngestRowProps {
  change: HomeChange;
  onOpenPage: (pages: HomePageRef[], label: string) => void;
}

// Mirrors Home.tsx's ChangeRow exactly: a change is only clickable when the backend resolved
// it to a real indexed document (scope, slug and title arrive together, or not at all — see
// HomeChange's comment in lib/types.ts). Everything else renders as plain, inert text.
function IngestRow({ change, onOpenPage }: IngestRowProps) {
  const title = change.title ?? change.message;
  const who = `${change.author_word}, ${relativeTime(change.when)}`;
  const dotClass = dotClassForAuthor(change.author_word);

  if (change.scope && change.slug && change.title) {
    const scope = change.scope;
    const slug = change.slug;
    const pageTitle = change.title;
    return (
      <button
        type="button"
        className="sources-row sources-row-clickable"
        onClick={() => onOpenPage([{ scope, slug, title: pageTitle }], `Added: ${pageTitle}`)}
        aria-label={`Open ${pageTitle}`}
      >
        <span className={`sources-dot ${dotClass}`} aria-hidden="true" />
        <div className="sources-row-body">
          <div className="sources-row-title">
            <RichTitle title={title} />
          </div>
          <div className="sources-row-note">{who}</div>
        </div>
      </button>
    );
  }

  return (
    <div className="sources-row sources-row-static">
      <span className={`sources-dot ${dotClass}`} aria-hidden="true" />
      <div className="sources-row-body">
        <div className="sources-row-title">
          <RichTitle title={title} />
        </div>
        <div className="sources-row-note">{who}</div>
      </div>
    </div>
  );
}

// ---- Slug derivation ----
//
// The mockup has no slug field, so one is derived from the title (or the dropped file's name)
// rather than asked for. slugify only ever emits lowercase ascii letters, digits and single
// hyphens with none leading/trailing, so its output can never contain a path separator, never
// equals "." or "..", is never empty, and never contains a control character — every rule
// internal/vault.ValidateSegment enforces server-side.
function slugify(input: string): string {
  const ascii = input.normalize("NFKD").replace(/[̀-ͯ]/g, "");
  const slug = ascii
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return slug || "source";
}

function fileNameBase(fileName: string | null): string {
  if (!fileName) return "";
  const dot = fileName.lastIndexOf(".");
  return dot > 0 ? fileName.slice(0, dot) : fileName;
}

// ---- Upload error copy ----

function describeUploadError(err: unknown, scope: string, nameHint: string): string {
  if (err instanceof ApiError) {
    if (err.status === 409) {
      return `A page named "${nameHint}" already exists in ${scope}. Try a different title.`;
    }
    if (err.status === 413) {
      return "That's larger than the 2 MB limit for a single drop.";
    }
    return err.message || "Could not add this source.";
  }
  return err instanceof Error ? err.message : "Could not add this source.";
}

// ---- Small local formatters (see the file header note on why these are not shared) ----

const MINUTE_MS = 60_000;
const HOUR_MINUTES = 60;
const DAY_HOURS = 24;
const MONTH_DAYS = 30;

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

function dotClassForAuthor(word: string): string {
  if (word === "you") return "sources-dot-you";
  if (word === "an agent") return "sources-dot-agent";
  return "sources-dot-connector";
}
