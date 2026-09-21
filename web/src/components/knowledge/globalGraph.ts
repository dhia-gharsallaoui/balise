import type { GraphEdge, GraphNode, GraphResponse } from "../../lib/types";

// Whole-vault graph (GraphView's no-centre branch): "the general global graph that show
// all" the owner asked for, alongside the existing ego view (unchanged — see ego.ts).
// Live data (139 nodes / 190 edges) is five scopes that never share an edge with each
// other (scope is the security boundary), so this reads as five islands rather than one
// hairball: group by scope first, and within each scope split nodes that have at least one
// relation ("connected", force-laid-out by the existing layoutGraph) from nodes that have
// none ("isolated" — shelved separately in a deterministic grid, see globalLayout.ts).
//
// A node above this degree is rendered as a small labelled "hub" landmark instead of a
// plain dot (globalLayout.ts / GlobalGraphNodes.tsx) — chosen from the live corpus so the
// labelled set stays genuinely small: degree>=5 is 26 nodes, degree>=6 is 18, degree>=7 is
// 12 (all 12 in "work" and "client-globex", the two scopes big enough to have any hubs at all).
export const HUB_DEGREE_THRESHOLD = 7;

export interface ScopePanel {
  scope: string;
  // Nodes with at least one relation within the filtered graph, uid-sorted.
  connected: GraphNode[];
  // Nodes with none, uid-sorted — see globalLayout.ts for how these are rendered.
  isolated: GraphNode[];
  // Edges with both endpoints in this scope.
  edges: GraphEdge[];
  // Every node's degree within the filtered graph (not just this panel's), so a caller can
  // apply HUB_DEGREE_THRESHOLD without recomputing it.
  degree: Map<string, number>;
}

export interface GlobalGraph {
  // Ordered by (connected.length + isolated.length) desc, then scope name asc — biggest
  // island first, deterministic tie-break, matching how the five real scopes were reported.
  panels: ScopePanel[];
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

  const panels: ScopePanel[] = [...byScope.entries()].map(([scope, scopedNodes]) => {
    const scopedUids = new Set(scopedNodes.map((n) => n.uid));
    const scopedEdges = edges.filter((e) => scopedUids.has(e.from_uid) && scopedUids.has(e.to_uid));
    const connected = scopedNodes.filter((n) => (degree.get(n.uid) ?? 0) > 0).sort(byUid);
    const isolated = scopedNodes.filter((n) => (degree.get(n.uid) ?? 0) === 0).sort(byUid);
    return { scope, connected, isolated, edges: scopedEdges, degree };
  });

  panels.sort((a, b) => {
    const aCount = a.connected.length + a.isolated.length;
    const bCount = b.connected.length + b.isolated.length;
    if (aCount !== bCount) return bCount - aCount;
    return a.scope.localeCompare(b.scope);
  });

  return {
    panels,
    totalNodes: nodes.length,
    totalEdges: edges.length,
    isolatedCount: panels.reduce((sum, p) => sum + p.isolated.length, 0),
    scopeCount: panels.length,
  };
}

function byUid(a: GraphNode, b: GraphNode): number {
  return a.uid.localeCompare(b.uid);
}
