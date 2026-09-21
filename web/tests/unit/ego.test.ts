import { describe, expect, it } from "vitest";
import { buildEgoGraph, MAX_EGO_NODES } from "../../src/components/knowledge/ego";
import type { GraphNode, GraphResponse } from "../../src/lib/types";

// Star + one depth-2 arm: c is the centre with three direct neighbours (n1, n2, n3);
// n1 has its own neighbour f1, two hops from c. `iso` shares no edge with anything and
// should never appear regardless of depth.
function node(uid: string): GraphNode {
  return { uid, slug: uid, title: `Title of ${uid}`, type: "gotcha", scope: "work" };
}

const STAR: GraphResponse = {
  nodes: ["c", "n1", "n2", "n3", "f1", "iso"].map(node),
  edges: [
    { from_uid: "c", to_uid: "n1", kind: "about" },
    { from_uid: "c", to_uid: "n2", kind: "link" },
    { from_uid: "n3", to_uid: "c", kind: "specializes" },
    { from_uid: "n1", to_uid: "f1", kind: "about" },
  ],
};

const CENTRE_REF = { scope: "work", slug: "c" };

describe("buildEgoGraph", () => {
  it("returns null when the centre page isn't in the graph", () => {
    expect(buildEgoGraph(STAR, { scope: "work", slug: "missing" }, 1)).toBeNull();
  });

  it("puts the centre node at depth 0", () => {
    const ego = buildEgoGraph(STAR, CENTRE_REF, 1)!;
    expect(ego.nodes.find((n) => n.uid === "c")?.depth).toBe(0);
  });

  it("depth 1 includes direct neighbours but not their neighbours", () => {
    const ego = buildEgoGraph(STAR, CENTRE_REF, 1)!;
    const uids = ego.nodes.map((n) => n.uid).sort();
    expect(uids).toEqual(["c", "n1", "n2", "n3"]);
    expect(ego.nodes.find((n) => n.uid === "n1")?.depth).toBe(1);
  });

  it("depth 2 pulls in neighbours of neighbours", () => {
    const ego = buildEgoGraph(STAR, CENTRE_REF, 2)!;
    const uids = ego.nodes.map((n) => n.uid).sort();
    expect(uids).toEqual(["c", "f1", "n1", "n2", "n3"]);
    expect(ego.nodes.find((n) => n.uid === "f1")?.depth).toBe(2);
  });

  it("never includes a node with no path to the centre", () => {
    const ego = buildEgoGraph(STAR, CENTRE_REF, 2)!;
    expect(ego.nodes.some((n) => n.uid === "iso")).toBe(false);
  });

  it("only keeps edges between two kept nodes", () => {
    const ego = buildEgoGraph(STAR, CENTRE_REF, 1)!;
    // the n1-f1 edge must be dropped: f1 isn't part of the depth-1 subgraph
    expect(ego.edges.some((e) => e.from_uid === "f1" || e.to_uid === "f1")).toBe(false);
    expect(ego.edges).toHaveLength(3);
  });

  it("reports the direct-neighbour count regardless of the depth requested", () => {
    expect(buildEgoGraph(STAR, CENTRE_REF, 1)!.directNeighbourCount).toBe(3);
    expect(buildEgoGraph(STAR, CENTRE_REF, 2)!.directNeighbourCount).toBe(3);
  });

  it("caps the rendered node count and reports how many were elided", () => {
    const many: GraphResponse = {
      nodes: ["c", ...Array.from({ length: 50 }, (_, i) => `n${i}`)].map(node),
      edges: Array.from({ length: 50 }, (_, i) => ({
        from_uid: "c",
        to_uid: `n${i}`,
        kind: "link",
      })),
    };
    const ego = buildEgoGraph(many, { scope: "work", slug: "c" }, 1)!;
    expect(ego.nodes.length).toBeLessThanOrEqual(MAX_EGO_NODES);
    expect(ego.nodes.length).toBe(MAX_EGO_NODES);
    expect(ego.omittedCount).toBe(50 - (MAX_EGO_NODES - 1));
  });

  it("produces the same node and edge order every time (layout determinism)", () => {
    const a = buildEgoGraph(STAR, CENTRE_REF, 2)!;
    const b = buildEgoGraph(STAR, CENTRE_REF, 2)!;
    expect(a.nodes.map((n) => n.uid)).toEqual(b.nodes.map((n) => n.uid));
    expect(a.edges).toEqual(b.edges);
  });

  it("always sorts the centre node first for a stable layout seed order", () => {
    const ego = buildEgoGraph(STAR, CENTRE_REF, 2)!;
    expect(ego.nodes[0].uid).toBe("c");
  });
});
