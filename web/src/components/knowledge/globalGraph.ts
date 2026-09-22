import type { GraphEdge, GraphNode, GraphResponse } from "../../lib/types";

// Whole-vault graph (GraphView's no-centre branch): "the general global graph that show
// all" the owner asked for, alongside the existing ego view (unchanged — see ego.ts).
// Live data (139 nodes / 190 edges) is five scopes that never share an edge with each
// other (scope is the security boundary), so this reads as five islands rather than one
// hairball — but where the old hand-rolled layout turned that fact into five separate
// force-simulated <svg> panels, the Cytoscape rewrite turns it into five compound
// ("parent") nodes on one shared canvas: a scope is a real container node other nodes sit
// inside, not a layout-only convention. That single-canvas shape needs a flatter data
// model than the old ScopePanel's connected/isolated split — fcose's `tile: true` already
// places degree-0 nodes sensibly on its own, so there is nothing left for this module to
// decide about isolation; it only groups, filters and measures.

export interface ScopeGroup {
  scope: string;
  // Every node in this scope (connected or not), uid-sorted.
  nodes: GraphNode[];
  // Edges with both endpoints in this scope — every edge in the corpus qualifies for
  // exactly one scope, since no edge crosses a scope boundary (verified against the live
  // corpus; enforced here defensively by filtering on membership, not asserted).
  edges: GraphEdge[];
}

export interface GlobalGraph {
  // Ordered by node count desc, then scope name asc — biggest island first, deterministic
  // tie-break, matching how the five real scopes were reported.
  scopes: ScopeGroup[];
  // Every node's degree within the filtered graph, keyed by uid — shared across scopes
  // since a uid belongs to exactly one scope, so one map serves every caller.
  degree: Map<string, number>;
  totalNodes: number;
  totalEdges: number;
  isolatedCount: number;
  scopeCount: number;
}

/**
 * Builds the whole-vault graph shown when no page is open. Unlike ego.ts's BFS around one
 * centre, this keeps every node the caller says is visible: `allowedUids`, when given,
 * restricts the result to that set — Knowledge.tsx derives it from its own visibleGroups
 * (space + type filters), cross-referenced by uid, since /api/graph's GraphNode carries no
 * `space` field to filter on directly. Omitting it (or passing null/undefined) shows every
 * node the payload has, for callers/tests that don't need filtering.
 */
export function buildGlobalGraph(graph: GraphResponse, allowedUids?: Set<string> | null): GlobalGraph {
  const nodes = allowedUids ? graph.nodes.filter((n) => allowedUids.has(n.uid)) : graph.nodes;
  const nodeUids = new Set(nodes.map((n) => n.uid));
  const edges = graph.edges.filter((e) => nodeUids.has(e.from_uid) && nodeUids.has(e.to_uid));

  const degree = new Map<string, number>();
  for (const node of nodes) degree.set(node.uid, 0);
  for (const edge of edges) {
    degree.set(edge.from_uid, (degree.get(edge.from_uid) ?? 0) + 1);
    degree.set(edge.to_uid, (degree.get(edge.to_uid) ?? 0) + 1);
  }

  const byScope = new Map<string, GraphNode[]>();
  for (const node of nodes) {
    const list = byScope.get(node.scope);
    if (list) list.push(node);
    else byScope.set(node.scope, [node]);
  }

  const scopes: ScopeGroup[] = [...byScope.entries()].map(([scope, scopedNodes]) => {
    const scopedUids = new Set(scopedNodes.map((n) => n.uid));
    const scopedEdges = edges.filter((e) => scopedUids.has(e.from_uid) && scopedUids.has(e.to_uid));
    return { scope, nodes: [...scopedNodes].sort(byUid), edges: scopedEdges };
  });

  scopes.sort((a, b) => {
    if (a.nodes.length !== b.nodes.length) return b.nodes.length - a.nodes.length;
    return a.scope.localeCompare(b.scope);
  });

  let isolatedCount = 0;
  for (const d of degree.values()) if (d === 0) isolatedCount += 1;

  return {
    scopes,
    degree,
    totalNodes: nodes.length,
    totalEdges: edges.length,
    isolatedCount,
    scopeCount: scopes.length,
  };
}

function byUid(a: GraphNode, b: GraphNode): number {
  return a.uid.localeCompare(b.uid);
}
