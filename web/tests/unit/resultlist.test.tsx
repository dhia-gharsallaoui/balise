import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ResultList } from "../../src/components/knowledge/ResultList";
import type { Group, PageRow as Row } from "../../src/lib/types";

// `space` added to satisfy the real PageRow type (see pagerow.test.tsx note).
const page = (slug: string, type: Group["type"]) => ({
  slug,
  uid: slug,
  title: `${type} page`,
  type,
  status: "active",
  scope: "work",
  space: "work",
  historical: false,
  claims_count: 0,
  tokens: 1,
  owner: null,
  tags: [],
  claims: [],
});

// Fixture only — never re-sort alphabetically. This deliberately lists gotcha
// before state to prove ResultList re-orders by the fixed canonical order
// (state, decision, gotcha, procedure, issue, incident, note, memory), not by
// the order it was handed. Browse-mode (query="") fixture — see ROWS below for
// search mode's separate, order-preserving fixture.
const GROUPS: Group[] = [
  { type: "gotcha", count: 1, pages: [page("g", "gotcha")] },
  { type: "state", count: 1, pages: [page("s", "state")] },
];

// FIX 1 (search-ranking-coverage-report.md): search mode renders `rows`, not `groups` — a
// flat, order-preserving list standing in for /api/search's already-ranked hits. Deliberately
// out of canonical type order (issue, then state, then gotcha) to prove ResultList never
// re-sorts it the way it re-sorts GROUPS above.
const ROWS: Row[] = [page("i", "issue"), page("s2", "state"), page("g2", "gotcha")];

describe("ResultList (browse mode, no query)", () => {
  it("orders groups State before Gotchas", () => {
    render(<ResultList groups={GROUPS} rows={[]} query="" coverage="ok" onOpen={vi.fn()} />);
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings[0]).toContain("state");
    expect(headings[1]).toContain("gotcha");
  });

  it("shows a count beside each group", () => {
    render(<ResultList groups={GROUPS} rows={[]} query="" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.getByRole("heading", { name: /state/ }).textContent).toContain("1");
  });

  it("says nothing about coverage when there is no query (browse mode)", () => {
    render(<ResultList groups={GROUPS} rows={[]} query="" coverage={null} onOpen={vi.fn()} />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("renders the browse empty state when there is no query", () => {
    render(<ResultList groups={[]} rows={[]} query="" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.getByText(/Nothing in this space yet/)).toBeTruthy();
  });

  it("never shows a type chip on a browse row (type stays on the group header only)", () => {
    render(<ResultList groups={GROUPS} rows={[]} query="" coverage="ok" onOpen={vi.fn()} />);
    expect(document.querySelector(".type-chip")).toBeNull();
  });
});

describe("ResultList (search mode, query present)", () => {
  it("renders hits in the exact order given, never re-grouped by type", () => {
    render(<ResultList groups={[]} rows={ROWS} query="x" coverage="ok" onOpen={vi.fn()} />);
    // No section headers at all in search mode — type moved to a per-row indicator instead.
    expect(screen.queryAllByRole("heading", { level: 3 })).toHaveLength(0);
    const titles = screen.getAllByText(/ page$/).map((el) => el.textContent);
    expect(titles).toEqual(["issue page", "state page", "gotcha page"]);
  });

  it("prints each row's type as a chip instead of a group header", () => {
    render(<ResultList groups={[]} rows={ROWS} query="x" coverage="ok" onOpen={vi.fn()} />);
    const chips = Array.from(document.querySelectorAll(".type-chip")).map(
      (el) => el.getAttribute("data-type"),
    );
    expect(chips).toEqual(["issue", "state", "gotcha"]);
  });

  it("renders v3's empty state when a search matches nothing", () => {
    render(<ResultList groups={[]} rows={[]} query="revenue" coverage="low" onOpen={vi.fn()} />);
    expect(screen.getByText(/Nothing matches/)).toBeTruthy();
    expect(screen.getByText(/Try fewer words, or turn on historical pages/)).toBeTruthy();
  });

  it("warns on low coverage even when there are hits", () => {
    render(<ResultList groups={[]} rows={ROWS} query="x" coverage="low" onOpen={vi.fn()} />);
    expect(screen.getByRole("status").textContent).toMatch(/little about this/i);
  });

  it("says nothing about coverage when it is ok", () => {
    render(<ResultList groups={[]} rows={ROWS} query="x" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("ignores stale browse groups while a query is active", () => {
    // Regression guard: rows drive rendering whenever query is non-empty, even if a stale
    // `groups` value from a previous browse fetch is still sitting in state.
    render(<ResultList groups={GROUPS} rows={ROWS} query="x" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.queryAllByRole("heading", { level: 3 })).toHaveLength(0);
    expect(screen.getAllByText(/ page$/)).toHaveLength(3);
  });
});
