import { describe, expect, it } from "vitest";
import { buildGlobalGraph } from "../../src/components/knowledge/globalGraph";
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
  it("groups nodes into one ScopeGroup per scope", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    expect(g.scopeCount).toBe(2);
    expect(g.scopes.map((s) => s.scope).sort()).toEqual(["client-globex", "work"]);
  });

  it("never lets an edge cross a scope group", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    for (const group of g.scopes) {
      const uids = new Set(group.nodes.map((n) => n.uid));
      for (const edge of group.edges) {
        expect(uids.has(edge.from_uid)).toBe(true);
        expect(uids.has(edge.to_uid)).toBe(true);
      }
    }
  });

  it("keeps every node in its scope's group regardless of degree, uid-sorted", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    const work = g.scopes.find((s) => s.scope === "work")!;
    // uid-sorted via localeCompare, not insertion order — "w-iso" sorts before "w1"
    // because "-" precedes digits.
    expect(work.nodes.map((n) => n.uid)).toEqual(["w-iso", "w1", "w2", "w3"]);

    const clientGlobex = g.scopes.find((s) => s.scope === "client-globex")!;
    expect(clientGlobex.nodes.map((n) => n.uid)).toEqual(["c-iso", "c1", "c2"]);
  });

  it("reports each node's degree via the shared degree map, zero for isolated nodes", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    expect(g.degree.get("w1")).toBe(1);
    expect(g.degree.get("w2")).toBe(2);
    expect(g.degree.get("w3")).toBe(1);
    expect(g.degree.get("w-iso")).toBe(0);
    expect(g.degree.get("c-iso")).toBe(0);
  });

  it("counts isolated nodes across every scope", () => {
    const g = buildGlobalGraph(TWO_SCOPES);
    expect(g.isolatedCount).toBe(2);
    expect(g.totalNodes).toBe(7);
    expect(g.totalEdges).toBe(3);
  });

  it("orders scopes by size descending, then scope name ascending", () => {
    const bigger: GraphResponse = {
      nodes: [...TWO_SCOPES.nodes, node("w4", "work"), node("w5", "work")],
      edges: TWO_SCOPES.edges,
    };
    const g = buildGlobalGraph(bigger);
    // work now has 6 nodes vs client-globex's 3 — work must sort first.
    expect(g.scopes[0].scope).toBe("work");
    expect(g.scopes[1].scope).toBe("client-globex");
  });

  it("restricts nodes (and their edges) to an allowedUids filter, when given", () => {
    const g = buildGlobalGraph(TWO_SCOPES, new Set(["w1", "w2", "w-iso"]));
    expect(g.totalNodes).toBe(3);
    expect(g.scopeCount).toBe(1);
    const work = g.scopes[0];
    expect(work.nodes.map((n) => n.uid)).toEqual(["w-iso", "w1", "w2"]);
    // w2-w3 edge is dropped because w3 is filtered out.
    expect(g.totalEdges).toBe(1);
    expect(g.degree.get("w2")).toBe(1);
  });

  it("shows nothing when allowedUids excludes every node", () => {
    const g = buildGlobalGraph(TWO_SCOPES, new Set());
    expect(g.totalNodes).toBe(0);
    expect(g.scopeCount).toBe(0);
    expect(g.scopes).toEqual([]);
  });
});
