import type { ReactNode } from "react";
import { ArrowRight, CheckCircle, ClockCounterClockwise, Tray } from "@phosphor-icons/react";
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

// Home is the one screen with a hero moment: the greeting. Everything under it is a working
// surface in two unequal columns. The left stacks the two short, actionable lists (waiting,
// attention); the right holds the longer "Changed recently" timeline, so the long list no
// longer sets the height of two mostly empty siblings. The summary sentence spells its numbers
// out as words; every other count on this screen stays a plain digit.
export function Home({ home, error, onOpenReview, onFilterAttention }: Props) {
  if (error) {
    return <EmptyState title="Could not reach the server." hint={`${error}. Is balise serve running?`} />;
  }
  if (!home) return null;

  const scopeGroups = groupByScope(home.waiting.proposals);
  const changeGroups = groupChanges(home.changes);

  return (
    <div className="home-page">
      <header className="home-hero">
        <p className="home-greeting">{greeting(home.owner)}</p>
        <p className="home-summary">{summarySentence(home, changeGroups.length)}</p>
      </header>

      <div className="home-grid">
        <div className="home-col">
          <section className="home-section" aria-labelledby="home-waiting-heading">
            <div className="home-section-head">
              <h2 id="home-waiting-heading">Waiting for you</h2>
              <CountChip count={home.waiting.total} noun="proposal" />
              <button type="button" className="btn btn-quiet btn-sm home-head-action" onClick={onOpenReview}>
                Open review
                <ArrowRight size={14} weight="regular" aria-hidden="true" />
              </button>
            </div>

            {scopeGroups.length === 0 ? (
              <QuietEmpty icon={<Tray size={16} aria-hidden="true" />} text="Nothing is waiting for review right now." />
            ) : (
              <>
                <ul className="home-list">
                  {scopeGroups.map((group) => (
                    <li key={group.scope}>
                      <button
                        type="button"
                        className="home-row-waiting"
                        onClick={onOpenReview}
                        aria-label={`${plural(group.count, waitingRowNoun(group))} waiting in ${group.scope} — open review`}
                      >
                        <span className="home-row-numeral">{group.count}</span>
                        <span className="home-row-label">{waitingRowLabel(group)}</span>
                        <span className="home-row-source">{group.scope}</span>
                      </button>
                    </li>
                  ))}
                </ul>
                <p className="home-section-foot">Nothing is applied until you accept it.</p>
              </>
            )}
          </section>

          <section className="home-section" aria-labelledby="home-attention-heading">
            <div className="home-section-head">
              <h2 id="home-attention-heading">Needs attention</h2>
              <CountChip count={home.attention.length} noun="item" />
            </div>

            {home.attention.length === 0 ? (
              <QuietEmpty icon={<CheckCircle size={16} aria-hidden="true" />} text="Nothing needs attention right now." />
            ) : (
              <ul className="home-list">
                {home.attention.map((item) => (
                  <li key={item.rule} className="home-row-attention">
                    <span className="home-mark-warn" aria-hidden="true" />
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
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>

        <section className="home-section home-changes" aria-labelledby="home-changes-heading">
          <div className="home-section-head">
            <h2 id="home-changes-heading">Changed recently</h2>
            <CountChip count={changeGroups.length} noun="page" />
            <span className="home-head-meta">{changesWindowLabel(home.changes)}</span>
          </div>

          {changeGroups.length === 0 ? (
            <QuietEmpty
              icon={<ClockCounterClockwise size={16} aria-hidden="true" />}
              text="No pages have changed recently."
            />
          ) : (
            <ol className="home-timeline">
              {changeGroups.map((group) => (
                <li key={group.key} className="home-timeline-item">
                  <ChangeEntry group={group} onOpenPage={onFilterAttention} />
                </li>
              ))}
            </ol>
          )}
        </section>
      </div>
    </div>
  );
}

// The chip shows the bare digit; the noun is still read out to assistive tech, so "138" next
// to a heading is announced as "138 proposals".
function CountChip({ count, noun }: { count: number; noun: string }) {
  const [, ...rest] = plural(count, noun).split(" ");
  return (
    <span className="count home-count">
      {count}
      <span className="sr-only"> {rest.join(" ")}</span>
    </span>
  );
}

function QuietEmpty({ icon, text }: { icon: ReactNode; text: string }) {
  return (
    <p className="home-empty">
      {icon}
      <span>{text}</span>
    </p>
  );
}

interface ChangeEntryProps {
  group: ChangeGroup;
  onOpenPage: (pages: HomePageRef[], label: string) => void;
}

// A change is only clickable when the backend could resolve it to a real indexed document
// (internal/api/home.go's resolveChangeRef fills scope/slug/title together, or not at all;
// see HomeChange's comment in lib/types.ts). Everything else (a memory-import commit, or any
// tracked file the indexer never turned into a page) renders as plain, inert text instead of
// a button that would do nothing when pressed.
function ChangeEntry({ group, onOpenPage }: ChangeEntryProps) {
  const { latest, count } = group;
  const title = latest.title ?? firstLine(latest.message);
  const tickClass = latest.author_word === "you" ? "home-tick home-tick-you" : "home-tick";
  const body = (
    <>
      <span className={tickClass} aria-hidden="true" />
      <span className="home-entry-title"><RichTitle title={title} /></span>
      <span className="home-entry-meta">
        {latest.scope && <span className="home-entry-scope">{latest.scope}</span>}
        <span>{`${latest.author_word}, ${relativeTime(latest.when)}`}</span>
        {count > 1 && <span className="home-entry-updates">{plural(count, "update")}</span>}
      </span>
    </>
  );

  if (latest.scope && latest.slug && latest.title) {
    const ref = { scope: latest.scope, slug: latest.slug, title: latest.title };
    return (
      <button
        type="button"
        className="home-entry home-entry-link"
        onClick={() => onOpenPage([ref], `Changed: ${ref.title}`)}
        aria-label={`Open ${ref.title}`}
      >
        {body}
      </button>
    );
  }

  return <div className="home-entry home-entry-static">{body}</div>;
}

// ---- Greeting and summary ----

function greeting(owner: string): string {
  const hour = new Date().getHours();
  const timeWord = hour < 12 ? "morning" : hour < 18 ? "afternoon" : "evening";
  const name = owner ? capitalize(owner) : "";
  return name ? `Good ${timeWord}, ${name}.` : `Good ${timeWord}.`;
}

// changedPages counts grouped timeline entries, not raw commits: eight saves to one page are
// one page that changed.
function summarySentence(home: HomeResponse, changedPages: number): string {
  const waiting = home.waiting.total;
  const attention = home.attention.length;
  const changes = changedPages;

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

interface ChangeGroup {
  key: string;
  latest: HomeChange;
  count: number;
}

// An agent that remembers into one daily page commits to it many times in a row; listing every
// commit buried the rest of the timeline under "claude-code -- 2026-09-24" eight times. Changes
// are grouped by the page they touch (scope/slug when resolved, the file path otherwise), in
// order of each page's most recent change, and the newest commit speaks for the group.
function groupChanges(changes: HomeChange[]): ChangeGroup[] {
  const groups = new Map<string, ChangeGroup>();
  for (const change of changes) {
    const key = change.scope && change.slug ? `${change.scope}/${change.slug}` : change.path;
    const existing = groups.get(key);
    if (!existing) {
      groups.set(key, { key, latest: change, count: 1 });
      continue;
    }
    const newer = timeOf(change.when) > timeOf(existing.latest.when) ? change : existing.latest;
    groups.set(key, { key, latest: newer, count: existing.count + 1 });
  }
  return [...groups.values()];
}

function timeOf(iso: string): number {
  const t = new Date(iso).getTime();
  return Number.isNaN(t) ? 0 : t;
}

// Commit bodies can run to paragraphs; the timeline only ever shows the subject line.
function firstLine(message: string): string {
  return message.split("\n", 1)[0].trim();
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
