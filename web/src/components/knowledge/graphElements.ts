import type { ElementDefinition } from "cytoscape";
import { stripBackticks } from "../../lib/richTitle";
import type { EgoGraph, EgoNode } from "./ego";
import type { GlobalGraph } from "./globalGraph";

// Turns this app's own graph shapes (EgoGraph from ego.ts, GlobalGraph from globalGraph.ts)
// into Cytoscape element definitions. Kept separate from both: ego.ts/globalGraph.ts know
// nothing about Cytoscape, and graphStyle.ts only maps `data()` fields to paint — this
// module is the seam between "our domain model" and "the library's node/edge shape".

export const MIN_NODE_SIZE = 22;
export const MAX_NODE_SIZE = 64;
const SIZE_FACTOR = 8;

/**
 * width = min(MAX, MIN + sqrt(degree) * FACTOR) — degree 0 gives the floor size, and the
 * square root keeps a handful of very high-degree hubs from swallowing the canvas (linear
 * scaling made the busiest node in the live corpus roughly 8x its neighbours' area; sqrt
 * keeps it readably bigger without dominating).
 */
export function nodeSize(degree: number): number {
  const raw = MIN_NODE_SIZE + Math.sqrt(Math.max(0, degree)) * SIZE_FACTOR;
  return Math.min(MAX_NODE_SIZE, raw);
}

const MIN_HUB_THRESHOLD = 3;

/**
 * A node "counts as a hub" (and so keeps its label visible even when zoomed out — see
 * graphStyle.ts's label cascade) once its degree clears mean + 1 stddev of every nonzero
 * degree in the graph. Adaptive rather than the old fixed HUB_DEGREE_THRESHOLD=7: a small
 * ego neighbourhood and the 65-node Work scope don't share a degree distribution, so one
 * constant was either too strict (nothing qualifies in a small graph) or too loose (half of
 * a dense scope qualifies). Floored at 3 so a graph of near-uniform low degree doesn't mark
 * everything a hub.
 */
export function computeHubThreshold(degrees: Iterable<number>): number {
  const nonzero = [...degrees].filter((d) => d > 0);
  if (nonzero.length === 0) return MIN_HUB_THRESHOLD;
  const mean = nonzero.reduce((sum, d) => sum + d, 0) / nonzero.length;
  const variance = nonzero.reduce((sum, d) => sum + (d - mean) ** 2, 0) / nonzero.length;
  const stddev = Math.sqrt(variance);
  return Math.max(MIN_HUB_THRESHOLD, Math.round(mean + stddev));
}

export function scopeNodeId(scope: string): string {
  return `scope:${scope}`;
}

/** Cytoscape element id for a page node — distinct namespace from scopeNodeId so a scope
 * name can never collide with a page uid. */
export function pageNodeId(uid: string): string {
  return `page:${uid}`;
}

/**
 * Ego mode: the centred page and its neighbourhood out to the chosen depth (ego.ts already
 * did the BFS/cap). No compound scope nodes here — an ego graph is one neighbourhood, not a
 * vault-wide grouping, and it may legitimately show nodes from more than one scope's
 * perspective... except it never does, in practice, since edges never cross scopes; but
 * this function doesn't assume that, it just doesn't group by scope at all.
 */
export function buildEgoElements(ego: EgoGraph): ElementDefinition[] {
  const degree = new Map<string, number>();
  for (const node of ego.nodes) degree.set(node.uid, 0);
  for (const edge of ego.edges) {
    degree.set(edge.from_uid, (degree.get(edge.from_uid) ?? 0) + 1);
    degree.set(edge.to_uid, (degree.get(edge.to_uid) ?? 0) + 1);
  }
  const hubThreshold = computeHubThreshold(degree.values());

  const nodes: ElementDefinition[] = ego.nodes.map((node: EgoNode) => {
    const d = degree.get(node.uid) ?? 0;
    const isCentre = node.depth === 0;
    return {
      data: {
        id: pageNodeId(node.uid),
        uid: node.uid,
        label: stripBackticks(node.title),
        rawTitle: node.title,
        scope: node.scope,
        slug: node.slug,
        type: node.type,
        depth: node.depth,
        degree: d,
        isCentre,
        labelAlways: isCentre || node.depth <= 1 || d >= hubThreshold,
        size: isCentre ? MAX_NODE_SIZE : nodeSize(d),
      },
      classes: isCentre ? "graph-node graph-node-centre" : "graph-node",
    };
  });

  const edges: ElementDefinition[] = ego.edges.map((edge, index) => ({
    data: {
      id: `edge:${index}:${edge.from_uid}:${edge.to_uid}`,
      source: pageNodeId(edge.from_uid),
      target: pageNodeId(edge.to_uid),
      kind: edge.kind,
    },
    classes: "graph-edge",
  }));

  return [...nodes, ...edges];
}

/**
 * Global mode: every visible scope becomes a compound parent node (a real container other
 * nodes sit inside, per Cytoscape's parent/child model) and every page becomes a child
 * whose `parent` is that scope's node id. A collapsed scope (`collapsedScopes`, GraphView's
 * own React state) omits its children entirely — and since no edge crosses a scope
 * boundary (globalGraph.ts's ScopeGroup guarantees this), that also removes every edge
 * touching them, with nothing left dangling.
 */
export function buildGlobalElements(
  globalGraph: GlobalGraph,
  collapsedScopes: ReadonlySet<string>,
): ElementDefinition[] {
  const hubThreshold = computeHubThreshold(globalGraph.degree.values());
  const elements: ElementDefinition[] = [];

  for (const group of globalGraph.scopes) {
    const collapsed = collapsedScopes.has(group.scope);
    elements.push({
      data: {
        id: scopeNodeId(group.scope),
        label: collapsed ? `${group.scope} (${group.nodes.length})` : group.scope,
        isScope: true,
        collapsed,
        scope: group.scope,
      },
      classes: collapsed ? "graph-scope graph-scope-collapsed" : "graph-scope",
    });
    if (collapsed) continue;

    for (const node of group.nodes) {
      const d = globalGraph.degree.get(node.uid) ?? 0;
      elements.push({
        data: {
          id: pageNodeId(node.uid),
          uid: node.uid,
          label: stripBackticks(node.title),
          rawTitle: node.title,
          scope: node.scope,
          slug: node.slug,
          type: node.type,
          degree: d,
          isolated: d === 0,
          labelAlways: d >= hubThreshold,
          size: nodeSize(d),
          parent: scopeNodeId(group.scope),
        },
        classes: "graph-node",
      });
    }

    group.edges.forEach((edge, index) => {
      elements.push({
        data: {
          id: `edge:${group.scope}:${index}:${edge.from_uid}:${edge.to_uid}`,
          source: pageNodeId(edge.from_uid),
          target: pageNodeId(edge.to_uid),
          kind: edge.kind,
        },
        classes: "graph-edge",
      });
    });
  }

  return elements;
}
