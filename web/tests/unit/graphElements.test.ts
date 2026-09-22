import { describe, expect, it } from "vitest";
import {
  buildEgoElements,
  buildGlobalElements,
  computeHubThreshold,
  MAX_NODE_SIZE,
  MIN_NODE_SIZE,
  nodeSize,
  SIZE_FACTOR,
  pageNodeId,
  scopeNodeId,
} from "../../src/components/knowledge/graphElements";
import { buildGlobalGraph } from "../../src/components/knowledge/globalGraph";
import type { EgoGraph } from "../../src/components/knowledge/ego";
import type { GraphNode, GraphResponse } from "../../src/lib/types";

function node(uid: string, scope: string): GraphNode {
  return { uid, slug: uid, title: `Title of ${uid}`, type: "gotcha", scope };
}

describe("nodeSize", () => {
  it("returns the floor size for degree 0", () => {
    expect(nodeSize(0)).toBe(MIN_NODE_SIZE);
  });

  it("grows with the square root of degree, not linearly", () => {
    const at4 = nodeSize(4); // sqrt(4) = 2
    const at16 = nodeSize(16); // sqrt(16) = 4 — double the sqrt, not 4x
    expect(at4).toBe(MIN_NODE_SIZE + 2 * SIZE_FACTOR);
    expect(at16).toBe(MIN_NODE_SIZE + 4 * SIZE_FACTOR);
  });

  it("never exceeds MAX_NODE_SIZE even for a very high degree", () => {
    expect(nodeSize(10_000)).toBe(MAX_NODE_SIZE);
  });

  it("treats a negative degree the same as zero rather than throwing", () => {
    expect(nodeSize(-5)).toBe(MIN_NODE_SIZE);
  });
});

describe("computeHubThreshold", () => {
  it("floors at 3 when every degree is zero", () => {
    expect(computeHubThreshold([0, 0, 0])).toBe(3);
  });

  it("floors at 3 when there is no data at all", () => {
    expect(computeHubThreshold([])).toBe(3);
  });

  it("rises above the floor for a graph with real spread in degree", () => {
    // mean=5.2, stddev ~ 5.53 over these nonzero degrees -> threshold well above 3.
    const threshold = computeHubThreshold([1, 1, 2, 3, 19]);
    expect(threshold).toBeGreaterThan(3);
  });

  it("ignores zero-degree nodes when computing the mean/stddev", () => {
    // Same nonzero values as above, padded with a pile of isolated nodes — the extra
    // zeros must not pull the threshold down, since isolated nodes carry no signal
    // about what counts as "well connected" in this graph.
    const withZeros = computeHubThreshold([0, 0, 0, 0, 0, 1, 1, 2, 3, 19]);
    const withoutZeros = computeHubThreshold([1, 1, 2, 3, 19]);
    expect(withZeros).toBe(withoutZeros);
  });
});

describe("scopeNodeId / pageNodeId", () => {
  it("keeps scope and page ids in disjoint namespaces so they can never collide", () => {
    expect(scopeNodeId("work")).toBe("scope:work");
    expect(pageNodeId("work")).toBe("page:work");
    expect(scopeNodeId("x")).not.toBe(pageNodeId("x"));
  });
});

function makeEgo(): EgoGraph {
  return {
    nodes: [
      { ...node("centre", "work"), depth: 0 },
      { ...node("n1", "work"), depth: 1 },
      { ...node("n2", "work"), depth: 1 },
      { ...node("n3", "work"), depth: 2 },
    ],
    edges: [
      { from_uid: "centre", to_uid: "n1", kind: "about" },
      { from_uid: "centre", to_uid: "n2", kind: "link" },
      { from_uid: "n2", to_uid: "n3", kind: "about" },
    ],
    centreUid: "centre",
    directNeighbourCount: 2,
    omittedCount: 0,
  };
}

describe("buildEgoElements", () => {
  it("emits one Cytoscape node per ego node, ided by pageNodeId, plus one element per edge", () => {
    const ego = makeEgo();
    const elements = buildEgoElements(ego);
    const nodeIds = elements.filter((el) => !("source" in el.data)).map((el) => el.data.id);
    expect(nodeIds.sort()).toEqual(
      ["centre", "n1", "n2", "n3"].map(pageNodeId).sort(),
    );
    const edgeEls = elements.filter((el) => "source" in el.data);
    expect(edgeEls).toHaveLength(3);
  });

  it("marks only the depth-0 node as the centre, with graph-node-centre class and max size", () => {
    const ego = makeEgo();
    const elements = buildEgoElements(ego);
    const centre = elements.find((el) => el.data.id === pageNodeId("centre"))!;
    expect(centre.classes).toContain("graph-node-centre");
    expect(centre.data.isCentre).toBe(true);
    expect(centre.data.size).toBe(MAX_NODE_SIZE);

    const other = elements.find((el) => el.data.id === pageNodeId("n1"))!;
    expect(other.classes).not.toContain("graph-node-centre");
    expect(other.data.isCentre).toBe(false);
  });

  it("always keeps the centre and depth-1 nodes labelled, regardless of degree", () => {
    const ego = makeEgo();
    const elements = buildEgoElements(ego);
    const centre = elements.find((el) => el.data.id === pageNodeId("centre"))!;
    const n1 = elements.find((el) => el.data.id === pageNodeId("n1"))!;
    const n3 = elements.find((el) => el.data.id === pageNodeId("n3"))!; // depth 2, degree 1
    expect(centre.data.labelAlways).toBe(true);
    expect(n1.data.labelAlways).toBe(true);
    // n3 is depth 2 with degree 1, well under any hub threshold (floor 3) — not always labelled.
    expect(n3.data.labelAlways).toBe(false);
  });

  it("does not create scope parent nodes — an ego graph is one neighbourhood, not grouped", () => {
    const elements = buildEgoElements(makeEgo());
    expect(elements.some((el) => el.data.isScope)).toBe(false);
    expect(elements.every((el) => el.data.parent === undefined)).toBe(true);
  });
});

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

describe("buildGlobalElements", () => {
  it("emits one compound scope node per scope, and every page node parented to it", () => {
    const global = buildGlobalGraph(TWO_SCOPES);
    const elements = buildGlobalElements(global, new Set());
    const scopeEls = elements.filter((el) => el.data.isScope);
    expect(scopeEls.map((el) => el.data.id).sort()).toEqual(
      ["work", "client-globex"].map(scopeNodeId).sort(),
    );

    const w1 = elements.find((el) => el.data.id === pageNodeId("w1"))!;
    expect(w1.data.parent).toBe(scopeNodeId("work"));
    const c1 = elements.find((el) => el.data.id === pageNodeId("c1"))!;
    expect(c1.data.parent).toBe(scopeNodeId("client-globex"));
  });

  it("omits a collapsed scope's children and their edges entirely, keeping only the scope node", () => {
    const global = buildGlobalGraph(TWO_SCOPES);
    const elements = buildGlobalElements(global, new Set(["work"]));

    const workScope = elements.find((el) => el.data.id === scopeNodeId("work"))!;
    expect(workScope.data.collapsed).toBe(true);
    expect(workScope.classes).toContain("graph-scope-collapsed");
    expect(workScope.data.label).toContain("(4)"); // w1, w2, w3, w-iso

    expect(elements.some((el) => el.data.id === pageNodeId("w1"))).toBe(false);
    expect(elements.some((el) => el.data.id === pageNodeId("w2"))).toBe(false);
    const edgeEls = elements.filter((el) => "source" in el.data);
    expect(edgeEls.every((el) => el.data.source !== pageNodeId("w1"))).toBe(true);

    // The other scope is untouched.
    expect(elements.some((el) => el.data.id === pageNodeId("c1"))).toBe(true);
  });

  it("never crosses a scope boundary with an edge (each edge's source and target share a parent)", () => {
    const global = buildGlobalGraph(TWO_SCOPES);
    const elements = buildGlobalElements(global, new Set());
    const parentOf = new Map(
      elements.filter((el) => !el.data.isScope && !("source" in el.data)).map((el) => [el.data.id, el.data.parent]),
    );
    for (const el of elements) {
      if (!("source" in el.data)) continue;
      expect(parentOf.get(el.data.source as string)).toBe(parentOf.get(el.data.target as string));
    }
  });

  it("marks isolated nodes (degree 0) so styling/a11y can flag them", () => {
    const global = buildGlobalGraph(TWO_SCOPES);
    const elements = buildGlobalElements(global, new Set());
    const isolated = elements.find((el) => el.data.id === pageNodeId("w-iso"))!;
    expect(isolated.data.isolated).toBe(true);
    expect(isolated.data.degree).toBe(0);
  });
});
