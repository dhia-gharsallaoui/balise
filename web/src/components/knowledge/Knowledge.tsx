import { useEffect, useState } from "react";
import { fetchPage, fetchPages, search } from "../../lib/api";
import type {
  Claim, ClaimStatus, Group, HomePageRef, Hit, PageDetail as PageDetailData, PageRef, PageRow,
  TreeResponse,
} from "../../lib/types";
import { current as currentUrl, onPopState, write as writeUrl } from "../../lib/urlState";
import { EmptyState } from "../ui/EmptyState";
import { GraphView } from "./GraphView";
import { PageReader } from "./PageDetail";
import { ResultList } from "./ResultList";
import { SpaceTree } from "./SpaceTree";
import "../../styles/knowledge.css";

type ViewMode = "List" | "Graph";
type Breakpoint = "narrow" | "medium" | "wide";

function classify(width: number): Breakpoint {
  if (width < 820) return "narrow";
  if (width < 1180) return "medium";
  return "wide";
}

// Tracks the same 820/1180 breakpoints as the rest of the shell so the browse grid can
// decide column count in JS. Only the narrow/not-narrow distinction is load-bearing now —
// opening a page no longer reserves a third column at any width (see the comment above
// `cols` in Knowledge below) — but the medium/wide split is kept in case a future browse
// layout wants it, rather than collapsing this into a plain boolean for no real gain.
function useBreakpoint(): Breakpoint {
  const [bp, setBp] = useState<Breakpoint>(() =>
    classify(typeof window !== "undefined" ? window.innerWidth : 1400),
  );

  useEffect(() => {
    if (typeof window === "undefined") return;
    const onResize = () => setBp(classify(window.innerWidth));
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  return bp;
}

interface KnowledgeProps {
  tree: TreeResponse;
  // Set when Home's "Needs attention" sends the viewer here for a specific rule: restricts
  // the list to just the pages that sentence named, and gives them a way back to everything.
  restrictTo?: { pages: HomePageRef[]; label: string } | null;
  onClearRestrict?: () => void;
}

export function Knowledge({ tree, restrictTo, onClearRestrict }: KnowledgeProps) {
  // Everything a link should carry is seeded from the address bar on first render, so
  // opening a shared URL reconstructs the view rather than resetting it. The writers below
  // push it back; see lib/urlState.ts for which params are in the URL and why.
  const [initial] = useState(currentUrl);
  const [space, setSpaceState] = useState<string | null>(initial.space);
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(() => new Set(initial.hiddenTypes));
  const [showHistorical, setShowHistoricalState] = useState(initial.historical);
  const [view, setViewState] = useState<ViewMode>(initial.view);
  const [query, setQueryState] = useState(initial.query);
  const [groups, setGroups] = useState<Group[]>([]);
  const [coverage, setCoverage] = useState<"ok" | "low" | null>(null);
  // A page is opened by (scope, slug): the same slug can name a different customer's page in
  // each scope, so the slug alone is not enough to fetch the right one.
  const [open, setOpenState] = useState<PageRef | null>(initial.page);
  const [page, setPage] = useState<PageDetailData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const breakpoint = useBreakpoint();

  // Writers split by whether the change is a navigation or an adjustment. Opening a page,
  // choosing a space and switching to the graph are things you expect the back button to
  // undo, so they push. Typing in the search box would otherwise push one history entry per
  // keystroke — burying the previous screen under a dozen entries and making back useless —
  // so it, and the filter toggles, replace instead.
  function setQuery(next: string) {
    setQueryState(next);
    writeUrl({ query: next }, true);
  }

  function setSpace(next: string | null) {
    setSpaceState(next);
    writeUrl({ space: next });
  }

  function setView(next: ViewMode) {
    setViewState(next);
    writeUrl({ view: next });
  }

  function setOpen(next: PageRef | null) {
    setOpenState(next);
    writeUrl({ page: next });
  }

  function setShowHistorical(next: boolean) {
    setShowHistoricalState(next);
    writeUrl({ historical: next }, true);
  }

  // Back/forward changes the URL without re-rendering us, so every param has to be read
  // back out. Restores all of them together: a history entry describes one whole view, and
  // restoring half of it would show a state the viewer was never actually in.
  useEffect(() => onPopState((p) => {
    setQueryState(p.query);
    setSpaceState(p.space);
    setViewState(p.view);
    setOpenState(p.page);
    setHiddenTypes(new Set(p.hiddenTypes));
    setShowHistoricalState(p.historical);
  }), []);

  useEffect(() => {
    let cancelled = false;
    if (query.trim()) {
      search(query, showHistorical)
        .then((res) => {
          if (cancelled) return;
          setGroups(groupHits(res.hits));
          setCoverage(res.coverage);
        })
        .catch((e: unknown) => !cancelled && setError(messageOf(e)));
    } else {
      fetchPages({ historical: showHistorical })
        .then((res) => {
          if (cancelled) return;
          setGroups(res.groups);
          setCoverage(null);
        })
        .catch((e: unknown) => !cancelled && setError(messageOf(e)));
    }
    return () => {
      cancelled = true;
    };
  }, [query, showHistorical]);

  const openScope = open?.scope ?? null;
  const openSlug = open?.slug ?? null;
  useEffect(() => {
    let cancelled = false;
    if (!openScope || !openSlug) {
      setPage(null);
      return;
    }
    fetchPage({ scope: openScope, slug: openSlug })
      .then((detail) => !cancelled && setPage(detail))
      .catch((e: unknown) => !cancelled && setError(messageOf(e)));
    return () => {
      cancelled = true;
    };
  }, [openScope, openSlug]);

  // Computes the next set from the current one rather than inside a setState updater: the
  // URL write is a side effect, and StrictMode invokes updaters twice in development.
  function toggleType(type: string) {
    const next = new Set(hiddenTypes);
    if (next.has(type)) next.delete(type);
    else next.add(type);
    setHiddenTypes(next);
    writeUrl({ hiddenTypes: [...next] }, true);
  }

  const visibleGroups = filterGroups(groups, space, hiddenTypes, restrictTo?.pages ?? null);
  // Graph view's global (no-centre) mode reuses these exact filters so List and Graph
  // never disagree about what's visible — GraphNode has no `space` field to filter on
  // directly (see globalGraph.ts), so this crosses the reference by uid instead.
  const allowedUids = new Set(visibleGroups.flatMap((g) => g.pages.map((p) => p.uid)));

  // Owner's own call, now settled (see .superpowers/sdd/2026-09-16-balise-slice-frontend/
  // reading-view-report.md): "opening a claim or decision or anything opens a column which
  // is not ideal for reading, not enough wide" — a page opened from the List view no longer
  // shares the screen with a narrow reader column at any width. It takes over as a dedicated,
  // full-width reading page instead, addressed by the same `?page=` param urlState.ts already
  // carried (nothing new invented there) and with its own back affordance. This retires the
  // old three-column layout entirely and the brief-vs-design disagreement over when the third
  // (400px) column showed — there is no third column any more, in List or Graph view, at any
  // width; batch-F-report.md's note is superseded by this one.
  //
  // Graph view (spec §6.7 F-61) is unaffected: it keeps its own ego/global graph surface, with
  // `open` as the ego centre exactly as before — no reader ever renders alongside it. Switching
  // the List/Graph toggle to List is what opens the reading page for whichever page the graph
  // is centred on; that falls out of the rule below for free, with no new state.
  if (view === "List" && open) {
    return <PageReader page={page} onBack={() => setOpen(null)} />;
  }

  const cols = breakpoint === "narrow" ? 1 : 2;

  return (
    <div className="kn-grid" data-cols={cols}>
      <SpaceTree
        tree={tree.spaces}
        types={tree.types}
        historicalCount={tree.historical_count}
        selectedSpace={space}
        hiddenTypes={hiddenTypes}
        showHistorical={showHistorical}
        onSelectSpace={setSpace}
        onToggleType={toggleType}
        onToggleHistorical={() => setShowHistorical(!showHistorical)}
        globalLint={tree.global_lint}
      />

      <div className="kn-main">
        {restrictTo ? (
          <div className="kn-filter-banner">
            <span>Filtered: {restrictTo.label}</span>
            <button type="button" onClick={onClearRestrict}>
              Clear
            </button>
          </div>
        ) : null}
        <div className="kn-toolbar">
          <input
            className="kn-search"
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search claims…"
            aria-label="Search claims"
          />
          <div className="kn-viewmode" role="group" aria-label="View mode">
            {(["List", "Graph"] as ViewMode[]).map((mode) => (
              <button
                key={mode}
                type="button"
                aria-pressed={view === mode}
                onClick={() => setView(mode)}
              >
                {mode}
              </button>
            ))}
          </div>
        </div>

        {error ? (
          <EmptyState title="Could not reach the server." hint={`${error}. Is balise serve running?`} />
        ) : view === "Graph" ? (
          <GraphView centre={open} onOpen={setOpen} allowedUids={allowedUids} />
        ) : (
          <ResultList groups={visibleGroups} query={query} coverage={coverage} onOpen={setOpen} />
        )}
      </div>
    </div>
  );
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown error";
}

// Space + type filtering happens client-side: /api/pages has no space/scope query
// parameter (confirmed against the live backend), and hidden types must drop a
// whole group rather than leaving an empty header behind. restrictTo (from Home's
// "Needs attention" links) narrows further, to just the (scope, slug) pairs named
// by one lint rule's findings.
function filterGroups(
  groups: Group[],
  space: string | null,
  hiddenTypes: Set<string>,
  restrictTo: HomePageRef[] | null,
): Group[] {
  const allowed = restrictTo ? new Set(restrictTo.map((p) => `${p.scope}/${p.slug}`)) : null;
  return groups
    .filter((g) => !hiddenTypes.has(g.type))
    .map((g) => {
      let pages = space ? g.pages.filter((p) => p.space === space) : g.pages;
      if (allowed) pages = pages.filter((p) => allowed.has(`${p.scope}/${p.slug}`));
      return { ...g, pages, count: pages.length };
    })
    .filter((g) => g.pages.length > 0);
}

// /api/search returns Hit[], a thinner shape than PageRow (no uid, space,
// historical, tokens, owner, tags, or per-claim status — only matched claim
// text and the page's own status). This synthesizes best-effort PageRow-shaped
// rows so search results can flow through the same ResultList/PageRow
// rendering path as browse mode; see batch-F-report.md for the fields this
// approximates.
function groupHits(hits: Hit[]): Group[] {
  const byType = new Map<string, Group>();
  for (const hit of hits) {
    const claimStatus = (hit.status ?? "active") as ClaimStatus;
    const claims: Claim[] = hit.matched_claims.map((text) => ({
      text,
      status: claimStatus,
      as_of: null,
    }));
    const row: PageRow = {
      slug: hit.slug,
      uid: hit.slug,
      title: hit.title,
      type: hit.type,
      status: hit.status,
      scope: hit.scope,
      space: "",
      historical: false,
      claims_count: claims.length,
      tokens: 0,
      owner: null,
      tags: [],
      claims,
    };
    const existing = byType.get(hit.type);
    if (existing) {
      existing.pages.push(row);
      existing.count += 1;
    } else {
      byType.set(hit.type, { type: hit.type, count: 1, pages: [row] });
    }
  }
  return [...byType.values()];
}
