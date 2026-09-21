import { plural } from "../../lib/plural";
import type { HomeChange, HomePageRef, HomeProposal, HomeResponse } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { RichTitle } from "../ui/RichTitle";
import "../../styles/home.css";

interface Props {
  home: HomeResponse | null;
  error: string | null;
  onOpenReview: () => void;
  onFilterAttention: (pages: HomePageRef[], label: string) => void;
}

// Home was rebuilt from 02-ui-design-v1.md section 5.1's plain-sentence "briefing" to the
// card-grid direction in /tmp/balise-design/knowledge-v3.html (lines 74-121) — the supersession
// recorded in docs/superpowers/specs/2026-09-16-balise-vertical-slice-design.md section 9. The
// v3 markup keeps digits everywhere except one place: the greeting's summary sentence, which
// spells its numbers out as words. Every other count on this screen (header badges, row
// numerals, list sizes) stays a plain digit, matching the source design exactly.
export function Home({ home, error, onOpenReview, onFilterAttention }: Props) {
  if (error) {
    return <EmptyState title="Could not reach the server." hint={`${error}. Is balise serve running?`} />;
  }
  if (!home) return null;

  const scopeGroups = groupByScope(home.waiting.proposals);

  return (
    <div className="home-page">
      <h1 className="home-greeting">{greeting(home.owner)}</h1>
      <p className="home-summary">{summarySentence(home)}</p>

      <div className="home-cards">
        <section className="home-card" aria-labelledby="home-waiting-heading">
          <div className="home-card-head">
            <h2 id="home-waiting-heading">Waiting for you</h2>
            <span className="home-card-count">{plural(home.waiting.total, "proposal")}</span>
            <button type="button" className="home-open-review" onClick={onOpenReview}>
              Open review <span aria-hidden="true">→</span>
            </button>
          </div>

          {scopeGroups.length === 0 ? (
            <p className="home-empty">Nothing is waiting for review right now.</p>
          ) : (
            <>
              {scopeGroups.map((group) => (
                <button
                  key={group.scope}
                  type="button"
                  className="home-row-waiting"
                  onClick={onOpenReview}
                  aria-label={`${plural(group.count, waitingRowNoun(group))} waiting in ${group.scope} — open review`}
                >
                  <span className="home-row-numeral">{group.count}</span>
                  <span className="home-row-label">{waitingRowLabel(group)}</span>
                  <span className="home-row-source">{group.scope}</span>
                </button>
              ))}
              <div className="home-card-foot">Nothing is applied until you accept it.</div>
            </>
          )}
        </section>

        <section className="home-card" aria-labelledby="home-attention-heading">
          <div className="home-card-head">
            <h2 id="home-attention-heading">Needs attention</h2>
            <span className="home-card-count">{plural(home.attention.length, "item")}</span>
          </div>

          {home.attention.length === 0 ? (
            <p className="home-empty">Nothing needs attention right now.</p>
          ) : (
            home.attention.map((item) => (
              <div key={item.rule} className="home-row home-row-attention">
                <span className="home-dot home-dot-warn" aria-hidden="true" />
                <div className="home-row-body">
                  <button
                    type="button"
                    className="home-link"
                    onClick={() => onFilterAttention(item.pages, item.sentence)}
                  >
                    {item.sentence}
                  </button>
                  <div className="home-row-note">{attentionNote(item.pages)}</div>
                </div>
              </div>
            ))
          )}
        </section>

        <section className="home-card" aria-labelledby="home-changes-heading">
          <div className="home-card-head">
            <h2 id="home-changes-heading">Changed recently</h2>
            <span className="home-card-count">{changesWindowLabel(home.changes)}</span>
          </div>

          {home.changes.length === 0 ? (
            <p className="home-empty">No pages have changed recently.</p>
          ) : (
            home.changes.map((change) => (
              <ChangeRow key={change.sha} change={change} onOpenPage={onFilterAttention} />
            ))
          )}
        </section>
      </div>
    </div>
  );
}

interface ChangeRowProps {
  change: HomeChange;
  onOpenPage: (pages: HomePageRef[], label: string) => void;
}

// A change is only clickable when the backend could resolve it to a real indexed document
// (internal/api/home.go's resolveChangeRef fills scope/slug/title together, or not at all —
// see HomeChange's comment in lib/types.ts). Everything else — a memory-import commit, or any
// tracked file the indexer never turned into a page — renders as plain, inert text instead of
// a button that would do nothing when pressed.
function ChangeRow({ change, onOpenPage }: ChangeRowProps) {
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
        className="home-row home-row-change"
        onClick={() => onOpenPage([{ scope, slug, title: pageTitle }], `Changed: ${pageTitle}`)}
        aria-label={`Open ${pageTitle}`}
      >
        <span className={`home-dot ${dotClass}`} aria-hidden="true" />
        <div className="home-row-body">
          <div className="home-row-title"><RichTitle title={title} /></div>
          <div className="home-row-note">{who}</div>
        </div>
      </button>
    );
  }

  return (
    <div className="home-row home-row-change home-row-static">
      <span className={`home-dot ${dotClass}`} aria-hidden="true" />
      <div className="home-row-body">
        <div className="home-row-title"><RichTitle title={title} /></div>
        <div className="home-row-note">{who}</div>
      </div>
    </div>
  );
}

// ---- Greeting and summary ----

function greeting(owner: string): string {
  const hour = new Date().getHours();
  const timeWord = hour < 12 ? "morning" : hour < 18 ? "afternoon" : "evening";
  const name = owner ? capitalize(owner) : "";
  return name ? `Good ${timeWord}, ${name}.` : `Good ${timeWord}.`;
}

function summarySentence(home: HomeResponse): string {
  const waiting = home.waiting.total;
  const attention = home.attention.length;
  const changes = home.changes.length;

  const waitingClause = waiting === 0
    ? "nothing is waiting for a decision"
    : `${spellOut(waiting)} ${waiting === 1 ? "change is" : "changes are"} waiting for a decision`;
  const attentionClause = attention === 0
    ? "nothing needs a look"
    : `${spellOut(attention)} ${attention === 1 ? "thing needs" : "things need"} a look`;
  const changesClause = changes === 0
    ? "nothing has changed recently"
    : `${spellOut(changes)} ${changes === 1 ? "page has changed" : "pages have changed"} recently`;

  const sentence = `${waitingClause}, ${attentionClause}, and ${changesClause}.`;
  return capitalize(sentence);
}

function capitalize(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toUpperCase() + s.slice(1);
}

const ONES = [
  "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
  "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen",
  "nineteen",
];
const TENS = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"];

// Covers 0-999, which is every count this screen can realistically show (a vault's proposal
// queue, finding count, or recent-change count). A number outside that range falls back to a
// plain digit rather than guessing at "thousand"-scale prose no one asked for.
function spellOut(n: number): string {
  if (!Number.isInteger(n) || n < 0) return String(n);
  if (n < 20) return ONES[n];
  if (n < 100) {
    const tens = Math.floor(n / 10);
    const rest = n % 10;
    return rest === 0 ? TENS[tens] : `${TENS[tens]}-${ONES[rest]}`;
  }
  if (n < 1000) {
    const hundreds = Math.floor(n / 100);
    const rest = n % 100;
    return rest === 0 ? `${ONES[hundreds]} hundred` : `${ONES[hundreds]} hundred ${spellOut(rest)}`;
  }
  return String(n);
}

// ---- Waiting for you: grouped by scope ----

interface ScopeGroup {
  scope: string;
  count: number;
  kinds: Set<string>;
}

// Grouped by scope, not by target: scope is already a first-class, user-visible grouping
// elsewhere (Rail.tsx's per-scope counts), and grouping the real 138 pending proposals this
// way yields five rows — neither one undifferentiated "138" total nor 138 near-identical rows.
function groupByScope(proposals: HomeProposal[]): ScopeGroup[] {
  const map = new Map<string, ScopeGroup>();
  for (const p of proposals) {
    const existing = map.get(p.scope);
    if (existing) {
      existing.count += 1;
      existing.kinds.add(p.kind);
    } else {
      map.set(p.scope, { scope: p.scope, count: 1, kinds: new Set([p.kind]) });
    }
  }
  return [...map.values()].sort((a, b) => b.count - a.count || a.scope.localeCompare(b.scope));
}

const KIND_LABELS: Record<string, [string, string]> = {
  new_page: ["new page", "new pages"],
  update: ["update", "updates"],
  relation: ["relation", "relations"],
  merge: ["merge", "merges"],
  classify: ["reclassification", "reclassifications"],
  split: ["split", "splits"],
  claims: ["claim proposal", "claim proposals"],
};

function kindLabel(kind: string, count: number): string {
  const pair = KIND_LABELS[kind];
  if (!pair) return count === 1 ? kind : `${kind}s`;
  return count === 1 ? pair[0] : pair[1];
}

// The row's label describes what kind of proposal is pending. When a scope's proposals are all
// one kind (true for every scope in the live vault today — each group is 100% "claims") the
// label names that kind; a scope with mixed kinds falls back to the generic noun rather than
// naming just one kind and misrepresenting the rest.
function waitingRowLabel(group: ScopeGroup): string {
  if (group.kinds.size === 1) {
    const [kind] = group.kinds;
    return kindLabel(kind, group.count);
  }
  return group.count === 1 ? "proposal" : "proposals";
}

function waitingRowNoun(group: ScopeGroup): string {
  if (group.kinds.size === 1) {
    const [kind] = group.kinds;
    return kindLabel(kind, 1);
  }
  return "proposal";
}

// ---- Needs attention ----

// A short, fixed-length note rather than the pages' own titles: this vault's page titles are
// full claim sentences (some over 150 characters), so naming them here would wrap the muted
// note line across several lines instead of the one it's meant to be.
function attentionNote(pages: HomePageRef[]): string {
  if (pages.length === 0) return "Affects no indexed page";
  return `Affects ${plural(pages.length, "page")}`;
}

// ---- Changed recently ----

function daysAgo(iso: string): number {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return 0;
  const ms = Date.now() - then;
  return Math.max(0, Math.floor(ms / (24 * 60 * 60 * 1000)));
}

// A real, computed window ("today", "last N days") rather than a hardcoded phrase — the oldest
// change in the list sets how far back the card's badge claims to cover.
function changesWindowLabel(changes: HomeChange[]): string {
  if (changes.length === 0) return "no changes";
  const oldest = changes.reduce((max, c) => Math.max(max, daysAgo(c.when)), 0);
  if (oldest === 0) return "today";
  if (oldest === 1) return "last day";
  return `last ${oldest} days`;
}

function dotClassForAuthor(word: string): string {
  if (word === "you") return "home-dot-you";
  if (word === "an agent") return "home-dot-agent";
  return "home-dot-connector";
}

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
