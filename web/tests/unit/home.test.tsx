import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Home } from "../../src/components/home/Home";
import type { HomeResponse } from "../../src/lib/types";

// Home: a greeting hero over a two-column grid (waiting + attention on the left, a "Changed
// recently" timeline on the right). These tests pin the section order, scope-grouped waiting
// rows, per-page grouping of changes, and that the one place numbers read as prose (the
// summary sentence) actually does, while everywhere else keeps digits. The topbar owns the
// page's <h1>, so the greeting is a paragraph and no heading on this screen is above h2.

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
    expect(screen.getByText("Good morning, Dhia.")).toBeTruthy();
    // The topbar renders the page's h1; the greeting must not compete with it.
    expect(screen.queryByRole("heading", { level: 1 })).toBeNull();
  });

  it("falls back to a plain greeting when the vault has no owner", () => {
    render(<Home home={EMPTY} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    expect(screen.getByText("Good morning.")).toBeTruthy();
  });

  it("spells out every number in the summary sentence, never a digit", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    const summary = screen.getByText(/waiting for a decision/i);
    expect(summary.textContent).toBe(
      "One hundred thirty-eight changes are waiting for a decision, two things need a look, and three pages have changed recently.",
    );
    expect(summary.textContent).not.toMatch(/[0-9]/);
  });

  it("keeps digits everywhere else: heading count chips show a digit, with the noun for screen readers", () => {
    render(<Home home={FULL} error={null} onOpenReview={noop} onFilterAttention={noop} />);
    const chips = [...document.querySelectorAll(".count")];
    expect(chips.map((c) => c.textContent)).toEqual(["138 proposals", "2 items", "3 pages"]);
    // Only the digit is visible; the noun sits in an sr-only span.
    expect(chips.map((c) => c.firstChild?.textContent)).toEqual(["138", "2", "3"]);
    expect(chips.every((c) => c.querySelector(".sr-only"))).toBe(true);
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

  it("groups repeated changes to one page into a single entry with an update count", () => {
    const hoursAgo = (h: number) => new Date(Date.now() - h * 60 * 60 * 1000).toISOString();
    const repeated = (sha: string, when: string) => ({
      sha,
      path: "ai-studio/memory/claude-code/2026-09-24.md",
      message: "remember: claude-code",
      author_word: "you",
      when,
      scope: "ai-studio",
      slug: "2026-09-24",
      title: "claude-code 2026-09-24",
    });
    const home: HomeResponse = {
      ...EMPTY,
      changes: [
        repeated("r1", hoursAgo(3)),
        repeated("r2", hoursAgo(5)),
        repeated("r3", hoursAgo(9)),
        {
          sha: "f1",
          path: "ai-studio/decisions/x.md",
          message: "feat: initial pages\n\nA long body that must never reach the screen.",
          author_word: "an agent",
          when: hoursAgo(30),
        },
      ],
    };
    render(<Home home={home} error={null} onOpenReview={noop} onFilterAttention={noop} />);

    const entry = screen.getByRole("button", { name: "Open claude-code 2026-09-24" });
    expect(entry.textContent).toContain("3 updates");
    // The latest of the three commits sets the entry's time.
    expect(entry.textContent).toContain("3 hours ago");
    expect(screen.getAllByText("claude-code 2026-09-24")).toHaveLength(1);
    // A commit with no resolved page shows only its subject line.
    expect(screen.getByText("feat: initial pages")).toBeTruthy();
    expect(screen.queryByText(/long body/)).toBeNull();
    // The summary counts pages that changed, not commits.
    expect(screen.getByText(/two pages have changed recently/i)).toBeTruthy();
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
