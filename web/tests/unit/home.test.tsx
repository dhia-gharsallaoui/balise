import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Home } from "../../src/components/home/Home";
import type { HomeResponse } from "../../src/lib/types";

// Home was rebuilt from 02-ui-design-v1.md section 5.1's plain-sentence "briefing" to the
// card-grid design in /tmp/balise-design/knowledge-v3.html (lines 74-121) — see
// docs/superpowers/specs/2026-09-16-balise-vertical-slice-design.md section 9. These tests
// check the new shape: three cards, scope-grouped waiting rows, and that the one place numbers
// must read as prose (the summary sentence) actually does, while everywhere else keeps digits.

const FULL: HomeResponse = {
  waiting: {
    total: 138,
    by_kind: [{ kind: "claims", count: 138 }],
    proposals: [
      { id: "p1", kind: "claims", scope: "work", target: "a.md", confidence: 0.9, created_by: "agent", status: "pending" },
      { id: "p2", kind: "claims", scope: "work", target: "b.md", confidence: 0.9, created_by: "agent", status: "pending" },
      { id: "p3", kind: "claims", scope: "work", target: "c.md", confidence: 0.9, created_by: "agent", status: "pending" },
      { id: "p4", kind: "claims", scope: "client-globex", target: "d.md", confidence: 0.9, created_by: "agent", status: "pending" },
      { id: "p5", kind: "claims", scope: "client-globex", target: "e.md", confidence: 0.9, created_by: "agent", status: "pending" },
      { id: "p6", kind: "update", scope: "client-acme", target: "f.md", confidence: 0.9, created_by: "agent", status: "pending" },
    ],
  },
  attention: [
    {
      rule: "dangling_ref",
      sentence: "5 links point at pages that do not exist",
      count: 5,
      pages: [{ scope: "work", slug: "foo", title: "Foo" }],
    },
    {
      rule: "missing_claims",
      sentence: "3 pages have no claims yet",
      count: 3,
      pages: [
        { scope: "work", slug: "a", title: "A" },
        { scope: "work", slug: "b", title: "B" },
      ],
    },
  ],
  changes: [
    {
      sha: "abc1",
      path: "client-globex/gotchas/alloy.md",
      message: "accept: claims alloy",
      author_word: "you",
      when: "2026-09-18T12:00:00Z",
      scope: "client-globex",
      slug: "alloy",
      title: "Alloy duplicate component kills telemetry",
    },
    {
      sha: "abc2",
      path: "work/memory/claude-code/2026-09-18.md",
      message: "remember: claude-code",
      author_word: "you",
      when: "2026-09-18T06:00:00Z",
    },
    {
      sha: "abc3",
      path: "client-acme/decisions/naming.md",
      message: "balise: import claude-code memory",
      author_word: "a connector",
      when: "2026-09-10T00:00:00Z",
      scope: "client-acme",
      slug: "naming",
      title: "Name Acme nodes consistently",
    },
  ],
  owner: "dhia",
};

const EMPTY: HomeResponse = {
  waiting: { total: 0, by_kind: [], proposals: [] },
  attention: [],
  changes: [],
  owner: "",
};

function noop() {
  /* unused in most assertions */
}

function setupUser() {
  return userEvent.setup();
}

describe("Home", () => {
  beforeEach(() => {
    // shouldAdvanceTime: true keeps the fake clock ticking in step with real elapsed time,
    // so user-event's internal setTimeout-based delays still resolve on their own — plain
    // vi.useFakeTimers() froze them and every test that clicked something timed out at 5000ms.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date("2026-09-18T09:00:00"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the three cards in order: Waiting for you, Needs attention, Changed recently", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    const headings = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(["Waiting for you", "Needs attention", "Changed recently"]);
  });

  it("greets by the real owner, time-aware, with no name hardcoded", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Good morning, Dhia.");
  });

  it("falls back to a plain greeting when the vault has no owner", () => {
    render(<Home home={EMPTY} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Good morning.");
  });

  it("spells out every number in the summary sentence, never a digit", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    const summary = screen.getByText(/waiting for a decision/i);
    expect(summary.textContent).toBe(
      "One hundred thirty-eight changes are waiting for a decision, two things need a look, and three pages have changed recently.",
    );
    expect(summary.textContent).not.toMatch(/[0-9]/);
  });

  it("keeps digits everywhere else: card badges and row numerals are not spelled out", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByText("138 proposals")).toBeTruthy();
    expect(screen.getByText("2 items")).toBeTruthy();
  });

  it("groups Waiting for you rows by scope, largest first, and opens review on click", async () => {
    const user = setupUser();
    const onOpenReview = vi.fn();
    render(<Home home={FULL} error={null} onOpenReview={onOpenReview} onFilterAttention={noop} />);

    const rows = screen.getAllByRole("button", { name: /waiting in .* — open review/i });
    expect(rows).toHaveLength(3);
    expect(rows[0].textContent).toContain("3");
    expect(rows[0].textContent).toContain("work");
    expect(rows[1].textContent).toContain("2");
    expect(rows[1].textContent).toContain("client-globex");
    expect(rows[2].textContent).toContain("1");
    expect(rows[2].textContent).toContain("client-acme");

    await user.click(rows[0]);
    expect(onOpenReview).toHaveBeenCalledTimes(1);
  });

  it("shows the exact reassurance sentence under Waiting for you", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByText("Nothing is applied until you accept it.")).toBeTruthy();
  });

  it("opens review from the header button too", async () => {
    const user = setupUser();
    const onOpenReview = vi.fn();
    render(<Home home={FULL} error={null} onOpenReview={onOpenReview} onFilterAttention={noop} />);
    // Exact name (not the /open review/i regex): the per-scope waiting rows' aria-labels
    // also contain the substring "open review", so a loose match would hit several buttons.
    await user.click(screen.getByRole("button", { name: "Open review" }));
    expect(onOpenReview).toHaveBeenCalledTimes(1);
  });

  it("renders the server-composed sentence for each attention rule and filters on click", async () => {
    const user = setupUser();
    const onFilterAttention = vi.fn();
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={onFilterAttention} />);
    const sentence = screen.getByRole("button", { name: "5 links point at pages that do not exist" });
    await user.click(sentence);
    expect(onFilterAttention).toHaveBeenCalledWith(FULL.attention[0].pages, FULL.attention[0].sentence);
    expect(screen.getByText("Affects 2 pages")).toBeTruthy();
  });

  it("opens the target page when a Changed recently row is navigable", async () => {
    const user = setupUser();
    const onFilterAttention = vi.fn();
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={onFilterAttention} />);
    const row = screen.getByRole("button", { name: "Open Alloy duplicate component kills telemetry" });
    await user.click(row);
    expect(onFilterAttention).toHaveBeenCalledWith(
      [{ scope: "client-globex", slug: "alloy", title: "Alloy duplicate component kills telemetry" }],
      "Changed: Alloy duplicate component kills telemetry",
    );
  });

  it("renders a non-navigable change as plain text, not a dead button", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    // No title to resolve (the commit touches a real tracked file that was never indexed as a
    // page), so the row falls back to the commit's own subject line — never the raw repo path,
    // which reads as noise to someone who can't navigate to it anyway.
    expect(screen.getByText("remember: claude-code")).toBeTruthy();
    expect(screen.queryByText("work/memory/claude-code/2026-09-18.md")).toBeNull();
    expect(screen.queryByRole("button", { name: /claude-code/ })).toBeNull();
  });

  it("says plainly when a card is empty instead of showing a zero", () => {
    render(<Home home={EMPTY} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByText(/nothing is waiting for review/i)).toBeTruthy();
    expect(screen.getByText(/nothing needs attention/i)).toBeTruthy();
    expect(screen.getByText(/no pages have changed/i)).toBeTruthy();
    expect(screen.queryByText("Nothing is applied until you accept it.")).toBeNull();
  });

  it("shows a reachability error instead of a broken home screen", () => {
    render(<Home home={null} error="fetch failed" onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByText(/could not reach the server/i)).toBeTruthy();
  });
});
