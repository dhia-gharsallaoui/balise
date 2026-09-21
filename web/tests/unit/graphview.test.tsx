import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MAX_EGO_NODES } from "../../src/components/knowledge/ego";

// Star graph: centre "a" (the page open in the reader) has two direct neighbours (b, c);
// b has its own neighbour "d", two hops from the centre. "iso" shares no edge with anyone.
const LONG_TITLE = "This is a claim-style headline long enough to need truncating in the UI";

function baseGraph() {
  return {
    nodes: [
      { uid: "a", slug: "a", title: "Page A is the centre of this graph", type: "gotcha", scope: "work" },
      { uid: "b", slug: "b", title: "Page B", type: "state", scope: "work" },
      { uid: "c", slug: "c", title: LONG_TITLE, type: "entity", scope: "work" },
      { uid: "d", slug: "d", title: "Page D", type: "state", scope: "work" },
      { uid: "iso", slug: "iso", title: "Isolated page", type: "note", scope: "work" },
    ],
    edges: [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "a", to_uid: "c", kind: "link" },
      { from_uid: "b", to_uid: "d", kind: "about" },
    ],
  };
}

const CENTRE = { scope: "work", slug: "a" };

// vi.doMock only affects the next dynamic import, and vi.resetModules() forces that
// import to re-evaluate the module graph instead of reusing a previously-mocked,
// cached instance of GraphView (which would otherwise keep the first test's fetchGraph
// mock bound forever). Each test therefore mocks its own graph fixture and re-imports
// GraphView fresh, with no cross-test bleed.
beforeEach(() => {
  vi.resetModules();
});

function mockGraph(graph: ReturnType<typeof baseGraph>) {
  vi.doMock("../../src/lib/api", () => ({ fetchGraph: vi.fn().mockResolvedValue(graph) }));
}

async function loadGraphView() {
  const mod = await import("../../src/components/knowledge/GraphView");
  return mod.GraphView;
}

describe("GraphView", () => {
  it("shows the whole-vault global graph, grouped by scope, when no page is open", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={null} onOpen={vi.fn()} />);
    // baseGraph is a single scope ("work"): one panel, named for that scope.
    await waitFor(() => expect(screen.getByRole("region", { name: /work scope/i })).toBeTruthy());
    expect(screen.getByRole("button", { name: /Page A is the centre/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Page B$/ })).toBeTruthy();
    // depth-2-only in the ego view, but global mode has no depth cap — "d" is connected, so it shows too.
    expect(screen.getByRole("button", { name: /^Page D$/ })).toBeTruthy();
    // "iso" shares no edge with anyone and is called out as isolated, not silently dropped.
    expect(screen.getByRole("button", { name: /Isolated page/ })).toBeTruthy();
    expect(screen.getAllByText(/isolated/i).length).toBeGreaterThan(0);
  });

  it("keeps the global graph honouring an allowedUids filter", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={null} onOpen={vi.fn()} allowedUids={new Set(["a", "b"])} />);
    await waitFor(() => expect(screen.getByRole("button", { name: /Page A is the centre/ })).toBeTruthy());
    expect(screen.getByRole("button", { name: /^Page B$/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Page D$/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Isolated page/ })).toBeNull();
  });

  it("centres the graph on the open page and renders its direct neighbours", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByRole("img", { name: /relation graph/i })).toBeTruthy());
    expect(screen.getByRole("button", { name: /Page A is the centre/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Page B$/ })).toBeTruthy();
    // depth-2 node "d" must not appear at the depth-1 default
    expect(screen.queryByRole("button", { name: /^Page D$/ })).toBeNull();
  });

  it("shows the direct neighbour count", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByText(/2 direct neighbours/i)).toBeTruthy());
  });

  it("reveals depth-2 nodes only after switching the depth control", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByRole("button", { name: /^Page B$/ })).toBeTruthy());
    expect(screen.queryByRole("button", { name: /^Page D$/ })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Depth 2" }));
    await waitFor(() => expect(screen.getByRole("button", { name: /^Page D$/ })).toBeTruthy());
  });

  it("shows a legend of only the edge roles present in the rendered subgraph", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("about")).toBeTruthy());
    expect(screen.getByText("link")).toBeTruthy();
    // "specializes" never appears in this fixture's edges
    expect(screen.queryByText("specializes")).toBeNull();
  });

  it("opens a page when its node is activated", async () => {
    mockGraph(baseGraph());
    const onOpen = vi.fn();
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={onOpen} />);
    await waitFor(() => screen.getByRole("button", { name: /^Page B$/ }));
    await userEvent.click(screen.getByRole("button", { name: /^Page B$/ }));
    expect(onOpen).toHaveBeenCalledWith({ scope: "work", slug: "b" });
  });

  it("labels the svg for assistive technology", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByRole("img", { name: /relation graph/i })).toBeTruthy());
  });

  it("keeps the full title reachable via aria-label even for a long neighbour title", async () => {
    mockGraph(baseGraph());
    const GV = await loadGraphView();
    render(<GV centre={CENTRE} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByRole("button", { name: LONG_TITLE })).toBeTruthy());
  });

  it("shows an empty state when the centre page has no relations at all", async () => {
    mockGraph({
      nodes: [{ uid: "solo", slug: "solo", title: "Solo page", type: "note", scope: "work" }],
      edges: [],
    });
    const GV = await loadGraphView();
    render(<GV centre={{ scope: "work", slug: "solo" }} onOpen={vi.fn()} />);
    expect(await screen.findByText(/no relations/i)).toBeTruthy();
  });

  it("says plainly when nodes are elided by the render cap", async () => {
    const many = {
      nodes: [
        { uid: "a", slug: "a", title: "Centre", type: "gotcha", scope: "work" },
        ...Array.from({ length: 50 }, (_, i) => ({
          uid: `n${i}`, slug: `n${i}`, title: `Neighbour ${i}`, type: "state", scope: "work",
        })),
      ],
      edges: Array.from({ length: 50 }, (_, i) => ({ from_uid: "a", to_uid: `n${i}`, kind: "link" })),
    };
    mockGraph(many);
    const GV = await loadGraphView();
    render(<GV centre={{ scope: "work", slug: "a" }} onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByText(/more not shown/i)).toBeTruthy());
    expect(screen.getAllByRole("button", { name: /^Neighbour \d+$/ }).length).toBeLessThanOrEqual(MAX_EGO_NODES - 1);
  });
});
