import { describe, expect, it } from "vitest";
import { layoutRadialForest } from "../../src/components/knowledge/treeLayout";

// Fix round: layoutGlobalGraph switched its connected-node layout from the general-purpose
// force simulation to this radial spanning-tree layout for Work/Client-Globex's density
// (see treeLayout.ts's header for why). These tests exercise the algorithm directly,
// independent of the scope-grouping/isolation tests in globalLayout.test.ts.

function node(uid: string) {
  return { uid, slug: uid, title: uid, type: "gotcha" as const, scope: "work" };
}

const BOX = { width: 480, height: 480, seed: 42 };

describe("layoutRadialForest", () => {
  it("is deterministic for a fixed seed", () => {
    const nodes = ["a", "b", "c", "d", "e"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "a", to_uid: "c", kind: "about" },
      { from_uid: "c", to_uid: "d", kind: "link" },
      { from_uid: "c", to_uid: "e", kind: "link" },
    ];
    const first = layoutRadialForest(nodes, edges, BOX);
    const second = layoutRadialForest(nodes, edges, BOX);
    expect(first.treeEdgeIndices).toEqual(second.treeEdgeIndices);
    for (const [uid, p] of first.positions) {
      expect(second.positions.get(uid)).toEqual(p);
    }
  });

  it("differs (rotates) for a different seed but keeps the same tree structure", () => {
    const nodes = ["a", "b", "c", "d", "e"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "a", to_uid: "c", kind: "about" },
      { from_uid: "c", to_uid: "d", kind: "link" },
      { from_uid: "c", to_uid: "e", kind: "link" },
    ];
    const seed42 = layoutRadialForest(nodes, edges, BOX);
    const seed7 = layoutRadialForest(nodes, edges, { ...BOX, seed: 7 });
    expect([...seed42.positions.values()]).not.toEqual([...seed7.positions.values()]);
    // The tree/cross-link classification is structural, not seed-dependent.
    expect(seed42.treeEdgeIndices).toEqual(seed7.treeEdgeIndices);
  });

  it("picks the highest-degree node as the root (radius 0, centre of the panel)", () => {
    // "a" has degree 4 (hub); everyone else has degree 1. Root must be "a".
    const nodes = ["a", "b", "c", "d", "e"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "a", to_uid: "c", kind: "about" },
      { from_uid: "a", to_uid: "d", kind: "about" },
      { from_uid: "a", to_uid: "e", kind: "about" },
    ];
    const { positions } = layoutRadialForest(nodes, edges, BOX);
    const a = positions.get("a")!;
    expect(a.x).toBeCloseTo(BOX.width / 2, 0);
    expect(a.y).toBeCloseTo(BOX.height / 2, 0);
  });

  it("classifies exactly one edge per non-root node as a tree edge (a true spanning tree)", () => {
    // A 5-node, 4-edge tree already (no cycles) — every edge must be a tree edge.
    const nodes = ["a", "b", "c", "d", "e"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "a", to_uid: "c", kind: "about" },
      { from_uid: "c", to_uid: "d", kind: "link" },
      { from_uid: "c", to_uid: "e", kind: "link" },
    ];
    const { treeEdgeIndices } = layoutRadialForest(nodes, edges, BOX);
    expect(treeEdgeIndices.size).toBe(4);
  });

  it("marks edges beyond a spanning tree as cross-links, not tree edges", () => {
    // A 4-node cycle: a-b-c-d-a. A spanning tree needs 3 edges; the 4th closes the cycle
    // and must be reported as a cross-link (not in treeEdgeIndices).
    const nodes = ["a", "b", "c", "d"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "b", to_uid: "c", kind: "about" },
      { from_uid: "c", to_uid: "d", kind: "about" },
      { from_uid: "d", to_uid: "a", kind: "about" },
    ];
    const { treeEdgeIndices } = layoutRadialForest(nodes, edges, BOX);
    expect(treeEdgeIndices.size).toBe(3);
    expect(treeEdgeIndices.size).toBeLessThan(edges.length);
  });

  it("handles multiple disconnected components without overlapping their positions", () => {
    const nodes = ["a", "b", "c", "d"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "c", to_uid: "d", kind: "about" },
    ];
    const { positions, treeEdgeIndices } = layoutRadialForest(nodes, edges, BOX);
    expect(positions.size).toBe(4);
    expect(treeEdgeIndices.size).toBe(2);
    const a = positions.get("a")!;
    const c = positions.get("c")!;
    expect(a.x === c.x && a.y === c.y).toBe(false);
  });

  it("keeps every position within the panel's width/height bounds", () => {
    const nodes = ["a", "b", "c", "d", "e", "f"].map(node);
    const edges = [
      { from_uid: "a", to_uid: "b", kind: "about" },
      { from_uid: "a", to_uid: "c", kind: "about" },
      { from_uid: "c", to_uid: "d", kind: "link" },
      { from_uid: "d", to_uid: "e", kind: "link" },
      { from_uid: "e", to_uid: "f", kind: "link" },
    ];
    const { positions } = layoutRadialForest(nodes, edges, BOX);
    for (const p of positions.values()) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(BOX.width);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThanOrEqual(BOX.height);
    }
  });

  it("returns nothing for an empty node list", () => {
    const { positions, treeEdgeIndices } = layoutRadialForest([], [], BOX);
    expect(positions.size).toBe(0);
    expect(treeEdgeIndices.size).toBe(0);
  });
});
