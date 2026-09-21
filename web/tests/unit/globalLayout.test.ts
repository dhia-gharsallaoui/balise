import { describe, expect, it } from "vitest";
import { buildGlobalGraph } from "../../src/components/knowledge/globalGraph";
import { layoutGlobalGraph } from "../../src/components/knowledge/globalLayout";
import type { GraphNode, GraphResponse } from "../../src/lib/types";

function node(uid: string, scope: string): GraphNode {
  return { uid, slug: uid, title: `Title of ${uid}`, type: "gotcha", scope };
}

const TWO_SCOPES: GraphResponse = {
  nodes: [
    node("w1", "work"),
    node("w2", "work"),
    node("w3", "work"),
    node("w-iso", "work"),
    node("c1", "client-globex"),
    node("c2", "client-globex"),
  ],
  edges: [
    { from_uid: "w1", to_uid: "w2", kind: "about" },
    { from_uid: "w2", to_uid: "w3", kind: "link" },
    { from_uid: "c1", to_uid: "c2", kind: "about" },
  ],
};

describe("layoutGlobalGraph", () => {
  it("is deterministic for a fixed seed", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const a = layoutGlobalGraph(g.panels, 42);
    const b = layoutGlobalGraph(g.panels, 42);
    expect(a.length).toBe(b.length);
    for (let i = 0; i < a.length; i++) {
      expect(a[i].scope).toBe(b[i].scope);
      for (const [uid, p] of a[i].connectedPositions) {
        expect(b[i].connectedPositions.get(uid)).toEqual(p);
      }
      for (const [uid, p] of a[i].isolatedPositions) {
        expect(b[i].isolatedPositions.get(uid)).toEqual(p);
      }
    }
  });

  it("differs for a different seed", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const a = layoutGlobalGraph(g.panels, 42);
    const b = layoutGlobalGraph(g.panels, 7);
    const work = a.find((p) => p.scope === "work")!;
    const workB = b.find((p) => p.scope === "work")!;
    expect([...work.connectedPositions.values()]).not.toEqual([...workB.connectedPositions.values()]);
  });

  it("produces one layout per scope panel, each with its own positions", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const layouts = layoutGlobalGraph(g.panels, 42);
    expect(layouts.map((l) => l.scope).sort()).toEqual(["client-globex", "work"]);
    const work = layouts.find((l) => l.scope === "work")!;
    // Connected: w1, w2, w3. Isolated: w-iso.
    expect(work.connectedPositions.size).toBe(3);
    expect(work.isolatedPositions.size).toBe(1);
  });

  it("places isolated nodes in their own grid, separate from the connected force layout", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const work = layoutGlobalGraph(g.panels, 42).find((l) => l.scope === "work")!;
    expect(work.isolatedPositions.has("w-iso")).toBe(true);
    expect(work.connectedPositions.has("w-iso")).toBe(false);
    // The grid layout reports a positive height for the one isolated node it laid out.
    expect(work.isolatedHeight).toBeGreaterThan(0);
  });

  it("reports an empty isolated region for a scope with no isolated nodes", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const clientGlobex = layoutGlobalGraph(g.panels, 42).find((l) => l.scope === "client-globex")!;
    expect(clientGlobex.isolatedPositions.size).toBe(0);
    expect(clientGlobex.isolatedHeight).toBe(0);
  });

  it("keeps each scope's connected positions within that panel's own width/height bounds", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const work = layoutGlobalGraph(g.panels, 42).find((l) => l.scope === "work")!;
    for (const p of work.connectedPositions.values()) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(work.width);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThanOrEqual(work.connectedHeight);
    }
  });

  it("filtering the input graph (allowedUids) changes which nodes get laid out", () => {
    const full = buildGlobalGraph(TWO_SCOPES);
    const filtered = buildGlobalGraph(TWO_SCOPES, new Set(["w1", "w2", "w-iso"]));
    const fullLayouts = layoutGlobalGraph(full.panels, 42);
    const filteredLayouts = layoutGlobalGraph(filtered.panels, 42);
    expect(fullLayouts.length).toBe(2);
    expect(filteredLayouts.length).toBe(1);
    const work = filteredLayouts[0];
    expect(work.connectedPositions.size).toBe(2);
    expect(work.isolatedPositions.size).toBe(1);
  });
});
