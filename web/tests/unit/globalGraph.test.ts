import { describe, expect, it } from "vitest";
import { buildGlobalGraph, HUB_DEGREE_THRESHOLD } from "../../src/components/knowledge/globalGraph";
import type { GraphNode, GraphResponse } from "../../src/lib/types";

// Two scopes that never share an edge (scope is the security boundary — see
// globalGraph.ts), each with one isolated (degree-0) node, so a single fixture exercises
// scope grouping, isolation, and cross-scope independence together.
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
    node("c-iso", "client-globex"),
  ],
  edges: [
    { from_uid: "w1", to_uid: "w2", kind: "about" },
    { from_uid: "w2", to_uid: "w3", kind: "link" },
    { from_uid: "c1", to_uid: "c2", kind: "about" },
  ],
};

describe("buildGlobalGraph", () => {
  it("groups nodes into one panel per scope", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    expect(g.scopeCount).toBe(2);
    expect(g.panels.map((p) => p.scope).sort()).toEqual(["client-globex", "work"]);
  });

  it("never lets an edge cross a scope panel", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    for (const panel of g.panels) {
      const uids = new Set([...panel.connected, ...panel.isolated].map((n) => n.uid));
      for (const edge of panel.edges) {
        expect(uids.has(edge.from_uid)).toBe(true);
        expect(uids.has(edge.to_uid)).toBe(true);
      }
    }
  });

  it("separates degree-0 nodes into `isolated`, keeping the rest in `connected`", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const work = g.panels.find((p) => p.scope === "work")!;
    expect(work.connected.map((n) => n.uid).sort()).toEqual(["w1", "w2", "w3"]);
    expect(work.isolated.map((n) => n.uid)).toEqual(["w-iso"]);

    const clientGlobex = g.panels.find((p) => p.scope === "client-globex")!;
    expect(clientGlobex.connected.map((n) => n.uid).sort()).toEqual(["c1", "c2"]);
    expect(clientGlobex.isolated.map((n) => n.uid)).toEqual(["c-iso"]);
  });

  it("counts isolated nodes across every panel", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    expect(g.isolatedCount).toBe(2);
    expect(g.totalNodes).toBe(7);
    expect(g.totalEdges).toBe(3);
  });

  it("orders panels by size descending, then scope name ascending", () => {
    const bigger: GraphResponse = {
      nodes: [...TWO_SCOPES.nodes, node("w4", "work"), node("w5", "work")],
      edges: TWO_SCOPES.edges,
    };
    const g = buildGlobalGraph(bigger);
    // work now has 6 nodes vs client-globex's 3 — work must sort first.
    expect(g.panels[0].scope).toBe("work");
    expect(g.panels[1].scope).toBe("client-globex");
  });

  it("restricts nodes (and their edges) to an allowedUids filter, when given", () => {
    const g = buildGlobalGraph(TWO_SCOPES, new Set(["w1", "w2", "w-iso"]));
    expect(g.totalNodes).toBe(3);
    expect(g.scopeCount).toBe(1);
    const work = g.panels[0];
    expect(work.connected.map((n) => n.uid).sort()).toEqual(["w1", "w2"]);
    expect(work.isolated.map((n) => n.uid)).toEqual(["w-iso"]);
    // w2-w3 edge is dropped because w3 is filtered out.
    expect(g.totalEdges).toBe(1);
  });

  it("shows nothing when allowedUids excludes every node", () => {
    const g = buildGlobalGraph(TWO_SCOPES, new Set());
    expect(g.totalNodes).toBe(0);
    expect(g.scopeCount).toBe(0);
    expect(g.panels).toEqual([]);
  });

  it("marks a node's degree at or above HUB_DEGREE_THRESHOLD as reachable via the degree map", () => {
    const hubNode = node("hub", "work");
    const leaves = Array.from({ length: HUB_DEGREE_THRESHOLD }, (_, i) => node(`leaf${i}`, "work"));
    const graph: GraphResponse = {
      nodes: [hubNode, ...leaves],
      edges: leaves.map((leaf) => ({ from_uid: hubNode.uid, to_uid: leaf.uid, kind: "link" })),
    };
    const g = buildGlobalGraph(graph);
    const work = g.panels[0];
    expect(work.degree.get("hub")).toBe(HUB_DEGREE_THRESHOLD);
    expect(work.degree.get("hub")! >= HUB_DEGREE_THRESHOLD).toBe(true);
  });
});
