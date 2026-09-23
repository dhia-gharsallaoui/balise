import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ResultList } from "../../src/components/knowledge/ResultList";
import type { Group } from "../../src/lib/types";

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
// the order it was handed.
const GROUPS: Group[] = [
  { type: "gotcha", count: 1, pages: [page("g", "gotcha")] },
  { type: "state", count: 1, pages: [page("s", "state")] },
];

describe("ResultList", () => {
  it("orders groups State before Gotchas", () => {
    render(<ResultList groups={GROUPS} query="" coverage="ok" onOpen={vi.fn()} />);
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings[0]).toContain("state");
    expect(headings[1]).toContain("gotcha");
  });

  it("shows a count beside each group", () => {
    render(<ResultList groups={GROUPS} query="" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.getByRole("heading", { name: /state/ }).textContent).toContain("1");
  });

  it("renders v3's empty state when a search matches nothing", () => {
    render(<ResultList groups={[]} query="revenue" coverage="low" onOpen={vi.fn()} />);
    expect(screen.getByText(/Nothing matches/)).toBeTruthy();
    expect(screen.getByText(/Try fewer words, or turn on historical pages/)).toBeTruthy();
  });

  it("warns on low coverage even when there are hits", () => {
    render(<ResultList groups={GROUPS} query="x" coverage="low" onOpen={vi.fn()} />);
    expect(screen.getByRole("status").textContent).toMatch(/little about this/i);
  });

  it("says nothing about coverage when it is ok", () => {
    render(<ResultList groups={GROUPS} query="x" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("says nothing about coverage when there is no query (browse mode)", () => {
    render(<ResultList groups={GROUPS} query="" coverage={null} onOpen={vi.fn()} />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("renders the browse empty state when there is no query", () => {
    render(<ResultList groups={[]} query="" coverage="ok" onOpen={vi.fn()} />);
    expect(screen.getByText(/Nothing in this space yet/)).toBeTruthy();
  });
});
