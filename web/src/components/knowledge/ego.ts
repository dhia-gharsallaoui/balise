import type { GraphEdge, GraphNode, GraphResponse, PageRef } from "../../lib/types";

// Ego-graph extraction (spec 01-design-spec-v0.4.md §6.7 F-61). The whole-corpus graph
// (138 nodes / 190 edges live) is a hairball — 02-ui-design-v1.md §9 cut graph view for
// exactly that reason. Centred instead on one page, this is a pure, deterministic BFS
// over the existing /api/graph payload: no server change is needed because that payload
// (uid/slug/title/type/scope per node, from_uid/to_uid/kind per edge) is already a
// superset of what /api/pages/{scope}/{slug}'s `relations` would give per neighbour
// (relations lacks uid/scope, so it can't safely key nodes across scopes or extend past
// depth 1 without N+1 fetches — see graph-report.md for the full trade-off note).

export const MAX_EGO_NODES = 40;

export type EgoDepth = 0 | 1 | 2;

export interface EgoNode extends GraphNode {
  depth: EgoDepth;
}

export interface EgoGraph {
  nodes: EgoNode[];
  edges: GraphEdge[];
  centreUid: string;
  // Count of depth-1 neighbours reachable from the centre, independent of the depth
  // setting or the render cap — this is what the UI shows so the user knows what a
  // depth-1 view is hiding even before switching to depth 2.
  directNeighbourCount: number;
  // How many BFS-reachable nodes (within the requested depth) were dropped by the
  // MAX_EGO_NODES cap. Zero when nothing was elided.
  omittedCount: number;
}

/**
 * Builds the bounded neighbourhood of `centre` out of the full graph payload:
 * BFS to `depth` hops, cap at MAX_EGO_NODES (keeping the best-connected nodes first,
 * ties broken by uid for determinism), then keep only edges between two kept nodes.
 * Returns null if `centre` isn't a node in `graph` (e.g. it belongs to a different
 * scope's graph, or hasn't loaded yet).
 */
export function buildEgoGraph(
  graph: GraphResponse,
  centre: PageRef,
  depth: 1 | 2,
): EgoGraph | null {
  const centreNode = graph.nodes.find((n) => n.scope === centre.scope && n.slug === centre.slug);
  if (!centreNode) return null;

  const adjacency = buildAdjacency(graph.edges);
  const depths = bfsDepths(centreNode.uid, adjacency, depth);
  const directNeighbourCount = countAtDepth(depths, 1);

  const degreeOf = (uid: string): number => adjacency.get(uid)?.length ?? 0;
  const candidates = [...depths.keys()]
    .filter((uid) => uid !== centreNode.uid)
    .sort((a, b) => depths.get(a)! - depths.get(b)! || degreeOf(b) - degreeOf(a) || a.localeCompare(b));

  const capacity = MAX_EGO_NODES - 1;
  const kept = candidates.slice(0, capacity);
  const omittedCount = candidates.length - kept.length;
  const keptUids = new Set([centreNode.uid, ...kept]);

  const nodeByUid = new Map(graph.nodes.map((n) => [n.uid, n]));
  const nodes: EgoNode[] = [...keptUids]
    .map((uid) => ({ ...nodeByUid.get(uid)!, depth: depths.get(uid)! as EgoDepth }))
    .sort((a, b) => a.depth - b.depth || a.uid.localeCompare(b.uid));

  const edges = graph.edges.filter((e) => keptUids.has(e.from_uid) && keptUids.has(e.to_uid));

  return { nodes, edges, centreUid: centreNode.uid, directNeighbourCount, omittedCount };
}

function countAtDepth(depths: Map<string, number>, target: number): number {
  let count = 0;
  for (const d of depths.values()) if (d === target) count += 1;
  return count;
}

function buildAdjacency(edges: GraphEdge[]): Map<string, string[]> {
  const adjacency = new Map<string, string[]>();
  for (const edge of edges) {
    addDirected(adjacency, edge.from_uid, edge.to_uid);
    addDirected(adjacency, edge.to_uid, edge.from_uid);
  }
  return adjacency;
}

function addDirected(adjacency: Map<string, string[]>, from: string, to: string): void {
  const existing = adjacency.get(from);
  if (existing) existing.push(to);
  else adjacency.set(from, [to]);
}

/** BFS out to maxDepth hops. Neighbour visitation order is sorted (uid) so the result
 * is a pure function of (centreUid, edges) — required for the layout to stay stable. */
function bfsDepths(
  centreUid: string,
  adjacency: Map<string, string[]>,
  maxDepth: number,
): Map<string, number> {
  const depths = new Map<string, number>([[centreUid, 0]]);
  let frontier = [centreUid];
  for (let d = 1; d <= maxDepth; d++) {
    const next: string[] = [];
    for (const uid of frontier) {
      const neighbours = [...(adjacency.get(uid) ?? [])].sort();
      for (const neighbour of neighbours) {
        if (depths.has(neighbour)) continue;
        depths.set(neighbour, d);
        next.push(neighbour);
      }
    }
    frontier = next;
  }
  return depths;
}
