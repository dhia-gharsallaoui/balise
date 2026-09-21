import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SpaceTree } from "../../src/components/knowledge/SpaceTree";
import type { SpaceNode, TypeName } from "../../src/lib/types";

// Fixture tree — never live counts. Shapes match /api/tree's SpaceNode, but the
// numbers and names are made up so this test stays valid across the concurrent
// scope-layout change (client-colo removal / multi-customer flag).
const TREE: SpaceNode[] = [
  {
    name: "Alpha",
    scope: "alpha-scope",
    count: 12,
    children: [
      { name: "Alpha Sub", scope: "alpha-scope", count: 5, children: [] },
    ],
  },
  {
    name: "Beta",
    scope: "beta-scope",
    count: 7,
    children: [],
  },
];

const TYPES: { name: TypeName; count: number }[] = [
  { name: "state", count: 3 },
  { name: "decision", count: 4 },
];

describe("SpaceTree", () => {
  it("renders top-level spaces with counts", () => {
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={() => {}}
        onToggleType={() => {}}
        onToggleHistorical={() => {}}
      />,
    );
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.getByText("12")).toBeTruthy();
    expect(screen.getByText("Beta")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
  });

  it("renders nested children", () => {
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={() => {}}
        onToggleType={() => {}}
        onToggleHistorical={() => {}}
      />,
    );
    expect(screen.getByText("Alpha Sub")).toBeTruthy();
    expect(screen.getByText("5")).toBeTruthy();
  });

  it("reports selected space via onSelectSpace", async () => {
    const user = userEvent.setup();
    const onSelectSpace = vi.fn();
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={onSelectSpace}
        onToggleType={() => {}}
        onToggleHistorical={() => {}}
      />,
    );
    await user.click(screen.getByText("Beta"));
    expect(onSelectSpace).toHaveBeenCalledWith("Beta");
  });

  it("renders one checkbox per type with count", () => {
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={() => {}}
        onToggleType={() => {}}
        onToggleHistorical={() => {}}
      />,
    );
    expect(screen.getByRole("checkbox", { name: /state/i })).toBeTruthy();
    expect(screen.getByRole("checkbox", { name: /decision/i })).toBeTruthy();
    expect(screen.getByText("3")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
  });

  it("reports type toggle via onToggleType", async () => {
    const user = userEvent.setup();
    const onToggleType = vi.fn();
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={() => {}}
        onToggleType={onToggleType}
        onToggleHistorical={() => {}}
      />,
    );
    await user.click(screen.getByRole("checkbox", { name: /state/i }));
    expect(onToggleType).toHaveBeenCalledWith("state");
  });

  it("shows historical toggle with count", () => {
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={() => {}}
        onToggleType={() => {}}
        onToggleHistorical={() => {}}
      />,
    );
    expect(screen.getByRole("checkbox", { name: /historical/i })).toBeTruthy();
    expect(screen.getByText("2")).toBeTruthy();
  });

  it("marks selected space as aria-current", () => {
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace="Beta"
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={() => {}}
        onToggleType={() => {}}
        onToggleHistorical={() => {}}
      />,
    );
    expect(screen.getByText("Beta").closest("button")?.getAttribute("aria-current")).toBe("true");
    expect(screen.getByText("Alpha").closest("button")?.getAttribute("aria-current")).toBe("false");
  });

  // A finding that names no document — the only kind an unparseable page can produce, since
  // it has no uid — has no row in any list and no page to open. Without a home here the page
  // was simply absent from the screen with nothing saying why.
  it("surfaces findings that name no document", () => {
    render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={vi.fn()}
        onToggleType={vi.fn()}
        onToggleHistorical={vi.fn()}
        globalLint={[
          { rule: "unparseable", severity: "error", detail: "work/gotchas/broken.md: bad yaml" },
        ]}
      />,
    );
    expect(screen.getByText("unparseable")).toBeTruthy();
    expect(screen.getByText(/work\/gotchas\/broken.md/)).toBeTruthy();
  });

  it("renders nothing for them when there are none", () => {
    const { container } = render(
      <SpaceTree
        tree={TREE}
        types={TYPES}
        historicalCount={2}
        selectedSpace={null}
        hiddenTypes={new Set()}
        showHistorical={false}
        onSelectSpace={vi.fn()}
        onToggleType={vi.fn()}
        onToggleHistorical={vi.fn()}
        globalLint={[]}
      />,
    );
    expect(container.querySelector(".kn-global-lint")).toBeNull();
  });
});
