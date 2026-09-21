import type { GraphEdge } from "../../lib/types";
import { resolveCollisions, rng, type Box, type LayoutNode, type Point } from "./layout";

// Radial spanning-tree layout for the global graph's dense, single-component scope panels
// (fix round: Work — 65 connected nodes / 97 edges / avg degree 2.98 — and Client-Globex — 41
// nodes — both read as an under-converged force layout: nodes spread near-uniformly, edges
// running corner to corner, no visible structure. A sparse, 66%-tree graph like Work's does
// not need a spring simulation to draw cleanly; it needs the tree drawn as a tree.
//
// Algorithm, per connected component:
//   1. Root = the component's highest-degree node (tie-break: lower uid), matching "BFS
//      from the highest-degree node".
//   2. BFS from the root. The edge that first reaches each node becomes a *tree edge*;
//      every other edge among these nodes (parallel multi-kind edges, or a link between two
//      already-visited nodes) is a *cross-link* — reported back so the caller can render it
//      as visually secondary (graph.css's [data-secondary="true"]).
//   3. Each node gets an angular slice proportional to its subtree's size (so a node with a
//      big subtree gets more of the circle to fan out into, never overlapping a sibling's
//      slice) and sits at radius (its BFS depth / the tree's max depth) along that slice's
//      centre angle. This is a closed-form assignment — no iteration, no convergence check,
//      unlike layoutGraph's spring simulation.
//   4. Multiple components in one panel (rare — this corpus's big scopes are each a single
//      component) are tiled left-to-right, largest first, each in its own angular column, so
//      they never overlap each other's rings.
//   5. layout.ts's resolveCollisions runs once at the end as a deterministic cleanup pass,
//      the same way the force layout uses it, for the rare pair of leaves whose slices are
//      thin enough to place them too close together.
//
// The whole thing is a pure function of (nodes, edges, seed): the seed only rotates each
// component's ring (a single random() call per component) so a different seed still
// produces a visibly different — but equally readable — picture, without reintroducing any
// simulation or non-determinism into the actual tree/cross-link structure or spacing.

export interface RadialForestLayout {
  positions: Map<string, Point>;
  // Indices into the `edges` array (matching the array GraphEdges.tsx iterates with the
  // same index) that are spanning-tree edges. Every other index among connected nodes is a
  // cross-link.
  treeEdgeIndices: Set<number>;
}

interface Adjacency {
  neighbor: string;
  edgeIndex: number;
}

interface Tree {
  root: string;
  // BFS discovery order (root first); reversing it gives a valid bottom-up (leaves-first)
  // processing order for the subtree-weight pass, since every child is discovered after its
  // parent.
  discoveryOrder: string[];
  children: Map<string, string[]>;
  depth: Map<string, number>;
  weight: Map<string, number>;
  treeEdgeIndices: Set<number>;
}

const MARGIN = 24;
const MIN_RADIUS = 20;

export function layoutRadialForest(
  nodes: LayoutNode[],
  edges: GraphEdge[],
  box: Box,
): RadialForestLayout {
  const positions = new Map<string, Point>();
  const treeEdgeIndices = new Set<number>();
  if (nodes.length === 0) return { positions, treeEdgeIndices };

  const adjacency = buildAdjacency(nodes, edges);
  const components = findComponents(nodes, adjacency);
  const trees = components
    .map((members) => buildTree(members, adjacency))
    .sort((a, b) => b.discoveryOrder.length - a.discoveryOrder.length || a.root.localeCompare(b.root));

  const availableWidth = Math.max(box.width - 2 * MARGIN, 40);
  const availableHeight = Math.max(box.height - 2 * MARGIN, 40);
  const totalNodes = trees.reduce((sum, t) => sum + t.discoveryOrder.length, 0) || 1;
  const random = rng(box.seed);

  let cursorX = MARGIN;
  for (const tree of trees) {
    const share = tree.discoveryOrder.length / totalNodes;
    const columnWidth = Math.max(availableWidth * share, 60);
    const cx = cursorX + columnWidth / 2;
    const cy = MARGIN + availableHeight / 2;
    const rx = Math.max(columnWidth / 2 - 10, MIN_RADIUS);
    const ry = Math.max(availableHeight / 2 - 10, MIN_RADIUS);
    const rotation = random() * Math.PI * 2;
    placeTree(tree, positions, cx, cy, rx, ry, rotation);
    for (const index of tree.treeEdgeIndices) treeEdgeIndices.add(index);
    cursorX += columnWidth;
  }

  resolveCollisions(nodes, positions, box);
  return { positions, treeEdgeIndices };
}

function buildAdjacency(nodes: LayoutNode[], edges: GraphEdge[]): Map<string, Adjacency[]> {
  const adjacency = new Map<string, Adjacency[]>();
  for (const node of nodes) adjacency.set(node.uid, []);
  edges.forEach((edge, edgeIndex) => {
    if (!adjacency.has(edge.from_uid) || !adjacency.has(edge.to_uid)) return;
    adjacency.get(edge.from_uid)!.push({ neighbor: edge.to_uid, edgeIndex });
    adjacency.get(edge.to_uid)!.push({ neighbor: edge.from_uid, edgeIndex });
  });
  // Sorted by neighbour uid so BFS visits neighbours in a fixed order regardless of the
  // input edges' original order — required for determinism (same nodes/edges/seed always
  // produce the same tree, the same cross-link set, and the same positions).
  for (const list of adjacency.values()) list.sort((a, b) => a.neighbor.localeCompare(b.neighbor));
  return adjacency;
}

function findComponents(nodes: LayoutNode[], adjacency: Map<string, Adjacency[]>): string[][] {
  const visited = new Set<string>();
  const components: string[][] = [];
  const ordered = [...nodes].sort((a, b) => a.uid.localeCompare(b.uid)).map((n) => n.uid);
  for (const start of ordered) {
    if (visited.has(start)) continue;
    const members: string[] = [];
    const stack = [start];
    visited.add(start);
    while (stack.length > 0) {
      const uid = stack.pop()!;
      members.push(uid);
      for (const { neighbor } of adjacency.get(uid) ?? []) {
        if (visited.has(neighbor)) continue;
        visited.add(neighbor);
        stack.push(neighbor);
      }
    }
    components.push(members);
  }
  return components;
}

function buildTree(members: string[], adjacency: Map<string, Adjacency[]>): Tree {
  let root = members[0];
  for (const uid of members) {
    const degree = (adjacency.get(uid) ?? []).length;
    const rootDegree = (adjacency.get(root) ?? []).length;
    if (degree > rootDegree || (degree === rootDegree && uid < root)) root = uid;
  }

  const depth = new Map<string, number>([[root, 0]]);
  const children = new Map<string, string[]>();
  const treeEdgeIndices = new Set<number>();
  const visited = new Set<string>([root]);
  const queue = [root];
  while (queue.length > 0) {
    const uid = queue.shift()!;
    const kids: string[] = [];
    for (const { neighbor, edgeIndex } of adjacency.get(uid) ?? []) {
      if (visited.has(neighbor)) continue;
      visited.add(neighbor);
      depth.set(neighbor, (depth.get(uid) ?? 0) + 1);
      treeEdgeIndices.add(edgeIndex);
      kids.push(neighbor);
      queue.push(neighbor);
    }
    children.set(uid, kids);
  }

  const discoveryOrder = [...visited];
  const weight = new Map<string, number>();
  for (let i = discoveryOrder.length - 1; i >= 0; i--) {
    const uid = discoveryOrder[i];
    const kids = children.get(uid) ?? [];
    weight.set(uid, 1 + kids.reduce((sum, kid) => sum + (weight.get(kid) ?? 1), 0));
  }

  return { root, discoveryOrder, children, depth, weight, treeEdgeIndices };
}

// Assigns every node an angular slice sized by its subtree weight (so a bushier subtree
// gets proportionally more of the circle) and places it at that slice's centre angle, at a
// radius proportional to its BFS depth. The root sits at the component's centre (radius 0).
function placeTree(
  tree: Tree,
  positions: Map<string, Point>,
  cx: number,
  cy: number,
  rx: number,
  ry: number,
  rotation: number,
): void {
  positions.set(tree.root, { x: cx, y: cy });
  const maxDepth = Math.max(1, ...tree.depth.values());
  const angleSpan = new Map<string, [number, number]>([[tree.root, [0, Math.PI * 2]]]);
  const queue = [tree.root];
  while (queue.length > 0) {
    const uid = queue.shift()!;
    const [a0, a1] = angleSpan.get(uid)!;
    const kids = tree.children.get(uid) ?? [];
    if (kids.length === 0) continue;
    const totalWeight = kids.reduce((sum, kid) => sum + (tree.weight.get(kid) ?? 1), 0);
    let cursor = a0;
    for (const kid of kids) {
      const span = (a1 - a0) * ((tree.weight.get(kid) ?? 1) / totalWeight);
      const kidStart = cursor;
      const kidEnd = cursor + span;
      const angle = (kidStart + kidEnd) / 2 + rotation;
      const r = (tree.depth.get(kid) ?? 1) / maxDepth;
      positions.set(kid, { x: cx + Math.cos(angle) * rx * r, y: cy + Math.sin(angle) * ry * r });
      angleSpan.set(kid, [kidStart, kidEnd]);
      queue.push(kid);
      cursor = kidEnd;
    }
  }
}
