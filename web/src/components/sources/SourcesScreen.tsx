import { useEffect, useRef, useState, type ChangeEvent, type DragEvent, type FormEvent } from "react";
import { plural } from "../../lib/plural";
import { ApiError, fetchSourcesLog, uploadSourcePage } from "../../lib/api";
import type { HomeChange, HomePageRef } from "../../lib/types";
import { Check, UploadSimple, X } from "@phosphor-icons/react";
import { RichTitle } from "../ui/RichTitle";
import "../../styles/sources.css";

// The Sources screen (AppShell's subtitle: "Where knowledge comes in"). One intro line, one
// panel, one log:
//
//   - The intro says plainly that no connectors exist in this build (no connectors table, no
//     sync jobs), instead of a whole card holding a zero. It never implies one is coming.
//   - "Add a source" is the one real feature: drop or choose a markdown/text file, or paste
//     text, pick a space, and it becomes a page committed to the vault (POST
//     /api/sources/pages, internal/api/sources.go). The whole panel is the drop target; the
//     native file input stays in the DOM (labelled, visually hidden) and a real button opens
//     it, so the browser's "Choose File / No file chosen" chrome never shows. There is no slug
//     field, so a URL-safe slug is derived client-side from the title (or file name) via
//     slugify() below, which can never emit a value internal/vault.ValidateSegment rejects.
//   - The ingest log is real git history (GET /api/sources/log, Home's s.homeChanges reused
//     server-side), not a fabricated feed.
//
// Copy rule (02 section 8): never say "uid", "index", "token" or "budget" on screen; say
// "page", "space" and "source" instead. relativeTime is a small local copy per screen, this
// codebase's convention (see AgentsScreen's identical note) for one-line formatters.

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
  const fileInputRef = useRef<HTMLInputElement>(null);

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

  function clearFile() {
    setContent("");
    setFileName(null);
    setFeedback({ kind: "idle" });
  }

  return (
    <div className="sources-page">
      <p className="sources-intro">
        No sources are connected, so pages arrive by dropping a file or pasting text below. Each
        one becomes a page in the space you choose.
      </p>

      <section
        className={`panel sources-add${isDragOver ? " is-dragover" : ""}`}
        aria-labelledby="sources-add-heading"
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <h2 id="sources-add-heading" className="sources-heading">
          Add a source
        </h2>

        <form className="sources-form" onSubmit={handleSubmit}>
          <div className="sources-dropzone">
            <UploadSimple size={18} weight="regular" aria-hidden="true" className="sources-dropzone-icon" />
            {fileName ? (
              <div className="sources-file">
                <span className="sources-file-name">{fileName}</span>
                <button
                  type="button"
                  className="btn btn-quiet btn-sm"
                  onClick={clearFile}
                  aria-label={`Remove ${fileName}`}
                >
                  <X size={14} aria-hidden="true" />
                  Remove
                </button>
              </div>
            ) : (
              <p className="sources-dropzone-copy">
                Drop a .md or .txt file, or{" "}
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => fileInputRef.current?.click()}
                >
                  choose one
                </button>
                <span className="sources-dropzone-limit">Up to 2 MB</span>
              </p>
            )}
            <label className="sr-only" htmlFor="sources-file-input">
              Choose a file
            </label>
            <input
              ref={fileInputRef}
              id="sources-file-input"
              className="sr-only"
              type="file"
              tabIndex={-1}
              accept=".md,.markdown,.txt,text/markdown,text/plain"
              onChange={handleFileChange}
            />
          </div>

          <label className="sources-field" htmlFor="sources-paste">
            <span className="sources-label">Or paste text</span>
            <textarea
              id="sources-paste"
              className="field sources-paste"
              value={content}
              onChange={(e) => {
                setContent(e.target.value);
                setFileName(null);
              }}
              placeholder="Paste markdown or plain text…"
              rows={7}
            />
          </label>

          <div className="sources-row-fields">
            <label className="sources-field" htmlFor="sources-scope">
              <span className="sources-label">Into</span>
              <select
                id="sources-scope"
                className="field"
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

            <label className="sources-field" htmlFor="sources-title">
              <span className="sources-label">Title</span>
              <input
                id="sources-title"
                className="field"
                type="text"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Taken from the text if left blank…"
              />
            </label>

            <button type="submit" className="btn btn-primary sources-submit" disabled={submitting || !scope}>
              {submitting ? "Adding…" : "Add to vault"}
            </button>
          </div>

          <UploadFeedback feedback={feedback} onOpenPage={onOpenPage ? openIngestedPage : undefined} />
        </form>
      </section>

      <IngestLog log={log} error={logError} onOpenPage={openIngestedPage} />
    </div>
  );
}

function UploadFeedback({
  feedback,
  onOpenPage,
}: {
  feedback: Feedback;
  onOpenPage?: (pages: HomePageRef[], label: string) => void;
}) {
  if (feedback.kind === "error") {
    return (
      <p className="sources-feedback sources-feedback-error" role="alert">
        {feedback.message}
      </p>
    );
  }
  if (feedback.kind !== "success") return null;
  const { scope, slug, title } = feedback;
  return (
    <p className="sources-feedback sources-feedback-success" role="status">
      <Check size={14} weight="bold" aria-hidden="true" className="sources-feedback-icon" />
      <span>
        Added &quot;{title}&quot; to <span className="sources-mono">{scope}</span>.
      </span>
      {onOpenPage ? (
        <button
          type="button"
          className="btn btn-quiet btn-sm"
          onClick={() => onOpenPage([{ scope, slug, title }], `Added: ${title}`)}
        >
          Open it
        </button>
      ) : null}
    </p>
  );
}

// ---- Ingest log ----
//
// A compact log rather than a card list: time on the left, message, author on the right,
// hairline rules between rows only. Consecutive commits with the same message and author
// (a memory import that ran several times, the same page re-added) collapse into one row
// with a count, so a burst of identical entries does not push everything else off screen.

const LOG_PREVIEW_ROWS = 8;

interface LogGroup {
  head: HomeChange;
  count: number;
}

function groupConsecutive(log: HomeChange[]): LogGroup[] {
  return log.reduce<LogGroup[]>((groups, change) => {
    const last = groups[groups.length - 1];
    if (last && sameEntry(last.head, change)) {
      return [...groups.slice(0, -1), { head: last.head, count: last.count + 1 }];
    }
    return [...groups, { head: change, count: 1 }];
  }, []);
}

function sameEntry(a: HomeChange, b: HomeChange): boolean {
  return (
    (a.title ?? a.message) === (b.title ?? b.message) &&
    a.author_word === b.author_word &&
    a.scope === b.scope &&
    a.slug === b.slug
  );
}

interface IngestLogProps {
  log: HomeChange[] | null;
  error: string | null;
  onOpenPage: (pages: HomePageRef[], label: string) => void;
}

function IngestLog({ log, error, onOpenPage }: IngestLogProps) {
  const [showAll, setShowAll] = useState(false);
  const groups = log ? groupConsecutive(log) : [];
  const visible = showAll ? groups : groups.slice(0, LOG_PREVIEW_ROWS);
  const hidden = groups.length - visible.length;

  return (
    <section className="sources-log" aria-labelledby="sources-log-heading">
      <div className="sources-log-head">
        <h2 id="sources-log-heading" className="sources-heading">
          Ingest log
        </h2>
        <span className="sources-meta">{log ? plural(log.length, "entry") : ""}</span>
      </div>

      {error ? (
        <p className="sources-empty">Could not load the ingest log: {error}</p>
      ) : log === null ? null : log.length === 0 ? (
        <p className="sources-empty">Nothing has been added yet.</p>
      ) : (
        <ul className="sources-log-list">
          {visible.map((group) => (
            <li key={group.head.sha}>
              <IngestRow change={group.head} count={group.count} onOpenPage={onOpenPage} />
            </li>
          ))}
        </ul>
      )}

      {hidden > 0 ? (
        <button type="button" className="btn btn-quiet btn-sm sources-show-all" onClick={() => setShowAll(true)}>
          Show {hidden} more
        </button>
      ) : null}
    </section>
  );
}

interface IngestRowProps {
  change: HomeChange;
  count: number;
  onOpenPage: (pages: HomePageRef[], label: string) => void;
}

// Same rule as Home.tsx's ChangeRow: a change is only clickable when the backend resolved it
// to a real indexed document (scope, slug and title arrive together, or not at all, see
// HomeChange's comment in lib/types.ts). Everything else renders as plain, inert text.
function IngestRow({ change, count, onOpenPage }: IngestRowProps) {
  const body = (
    <>
      <time className="sources-log-time" dateTime={change.when}>
        {relativeTime(change.when)}
      </time>
      <span className="sources-log-msg">
        <span className="sources-log-title">
          <RichTitle title={change.title ?? change.message} />
        </span>
        {count > 1 ? (
          <span className="count">
            {count}
            <span className="sr-only"> times</span>
          </span>
        ) : null}
      </span>
      <span className="sources-log-who">{change.author_word}</span>
    </>
  );

  if (change.scope && change.slug && change.title) {
    const ref = { scope: change.scope, slug: change.slug, title: change.title };
    return (
      <button
        type="button"
        className="sources-log-row is-link"
        onClick={() => onOpenPage([ref], `Added: ${ref.title}`)}
        aria-label={`Open ${ref.title}`}
      >
        {body}
      </button>
    );
  }
  return <div className="sources-log-row">{body}</div>;
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
