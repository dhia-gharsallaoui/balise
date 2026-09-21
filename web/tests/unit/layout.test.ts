import { describe, expect, it } from "vitest";
import { estimateNodeBox, layoutGraph, type Point } from "../../src/components/knowledge/layout";

const NODES = ["a", "b", "c", "d"].map((slug) => ({
  uid: slug,
  slug,
  title: slug,
  type: "gotcha" as const,
  scope: "work",
}));
const EDGES = [
  { from_uid: "a", to_uid: "b", kind: "about" },
  { from_uid: "b", to_uid: "c", kind: "about" },
];
const BOX = { width: 860, height: 560, seed: 42 };

describe("layoutGraph", () => {
  it("positions every node", () => {
    expect(layoutGraph(NODES, EDGES, BOX).size).toBe(4);
  });

  it("keeps nodes inside the viewbox with a margin", () => {
    for (const p of layoutGraph(NODES, EDGES, BOX).values()) {
      expect(p.x).toBeGreaterThanOrEqual(20);
      expect(p.x).toBeLessThanOrEqual(840);
      expect(p.y).toBeGreaterThanOrEqual(20);
      expect(p.y).toBeLessThanOrEqual(540);
    }
  });

  it("is deterministic for the same seed", () => {
    const a = layoutGraph(NODES, EDGES, BOX);
    const b = layoutGraph(NODES, EDGES, BOX);
    for (const [uid, p] of a) expect(b.get(uid)).toEqual(p);
  });

  it("differs for a different seed", () => {
    const a = layoutGraph(NODES, EDGES, BOX);
    const b = layoutGraph(NODES, EDGES, { ...BOX, seed: 7 });
    expect([...a.values()]).not.toEqual([...b.values()]);
  });

  it("places connected nodes closer than unconnected ones", () => {
    const p = layoutGraph(NODES, EDGES, BOX);
    const dist = (x: string, y: string) =>
      Math.hypot(p.get(x)!.x - p.get(y)!.x, p.get(x)!.y - p.get(y)!.y);
    expect(dist("a", "b")).toBeLessThan(dist("a", "d"));
  });

  it("handles an empty graph", () => {
    expect(layoutGraph([], [], BOX).size).toBe(0);
  });

  it("handles a single node", () => {
    expect(layoutGraph([NODES[0]], [], BOX).size).toBe(1);
  });
});

// Regression coverage for the ego-graph overlap bug: at depth 1/2 a star topology (one
// centre, many siblings with no edges between them) gave the spring simulation nothing
// to keep siblings apart beyond point-repulsion, so rendered pills (which have real
// width/height, not zero) visually overlapped. The fix adds a deterministic
// collision-resolution pass, sized from the same per-depth box the pills render at
// (graph.css's data-depth rules) — proven here two ways: laying out twice and comparing
// (determinism), and checking every pair of estimated bounding boxes for overlap.
describe("layoutGraph — no overlap in a dense ego star (depth 1 and depth 2)", () => {
  const centre = {
    uid: "c", slug: "c", title: "Centre page with a longer headline", type: "gotcha" as const,
    scope: "work", depth: 0 as const,
  };
  const depth1 = Array.from({ length: 8 }, (_, i) => ({
    uid: `n${i}`, slug: `n${i}`, title: `Neighbour headline number ${i}`, type: "state" as const,
    scope: "work", depth: 1 as const,
  }));
  const depth2 = Array.from({ length: 6 }, (_, i) => ({
    uid: `f${i}`, slug: `f${i}`, title: `Far neighbour ${i}`, type: "entity" as const,
    scope: "work", depth: 2 as const,
  }));
  const STAR_NODES = [centre, ...depth1, ...depth2];
  const STAR_EDGES = [
    ...depth1.map((n) => ({ from_uid: centre.uid, to_uid: n.uid, kind: "about" })),
    ...depth2.map((n, i) => ({ from_uid: depth1[i % depth1.length].uid, to_uid: n.uid, kind: "about" })),
  ];
  const STAR_BOX = { width: 860, height: 560, seed: 42 };

  function boundingBoxes(positions: Map<string, Point>) {
    return STAR_NODES.map((n) => {
      const p = positions.get(n.uid)!;
      const size = estimateNodeBox(n);
      return {
        uid: n.uid,
        left: p.x - size.width / 2,
        right: p.x + size.width / 2,
        top: p.y - size.height / 2,
        bottom: p.y + size.height / 2,
      };
    });
  }

  function boxesOverlap(a: ReturnType<typeof boundingBoxes>[number], b: ReturnType<typeof boundingBoxes>[number]) {
    return a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
  }

  it("lays out the same star graph twice and produces identical positions", () => {
    const a = layoutGraph(STAR_NODES, STAR_EDGES, STAR_BOX);
    const b = layoutGraph(STAR_NODES, STAR_EDGES, STAR_BOX);
    expect(a.size).toBe(STAR_NODES.length);
    for (const [uid, p] of a) expect(b.get(uid)).toEqual(p);
  });

  it("never lets two node pills' estimated bounding boxes overlap", () => {
    const positions = layoutGraph(STAR_NODES, STAR_EDGES, STAR_BOX);
    const boxes = boundingBoxes(positions);
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        expect(boxesOverlap(boxes[i], boxes[j])).toBe(false);
      }
    }
  });
});
