import type { GraphEdge, GraphNode } from "../../lib/types";

// Deterministic force-directed layout for the relation graph (Batch G, Task 7).
// v3's static seed data hard-coded node coordinates; real data needs a computed
// layout, and it must be a pure function of (nodes, edges, seed) or every
// visual-regression screenshot of the graph would be flaky — see layout.test.ts's
// "is deterministic for the same seed" test.
//
// The spring simulation below treats every node as a zero-size point, so it only ever
// keeps nodes apart by repulsion strength — it has no idea a rendered node is actually a
// ~170x26px pill (graph.css), or that a star topology (one centre, many depth-1 siblings
// with no edges between them) gives it nothing but that repulsion to separate them. At
// depth 1/2 that reliably produced visually overlapping pills. The fix is a second,
// deterministic pass (resolveCollisions) that runs after the force simulation settles: it
// treats each node as an axis-aligned box sized from its depth (estimateNodeBox, kept in
// sync with graph.css's data-depth rules) and nudges any overlapping pair apart along
// whichever axis needs the smaller push, in a fixed sorted-by-uid iteration order — so the
// same (nodes, edges, seed) always still produces the same (now non-overlapping) layout.

export interface Point {
  x: number;
  y: number;
}

export interface Box {
  width: number;
  height: number;
  seed: number;
}

export interface NodeSize {
  width: number;
  height: number;
}

// A layout node only needs an optional depth: plain GraphNode (no depth) falls back to
// the depth-1 box size, and EgoNode (depth: 0 | 1 | 2) satisfies this exactly.
export type LayoutNode = GraphNode & { depth?: 0 | 1 | 2; box?: NodeSize };

const MARGIN = 20;
const ITERATIONS = 180;
const REPULSION = 42_000;
const SPRING = 0.008;
const IDEAL = 130;
const DAMPING = 0.82;

// Conservative (worst-case label length) pill footprints per depth, derived from
// graph.css: padding + dot + gap + that depth's .graph-node-label max-width. Using the
// max rather than a measured width keeps this a pure function of (uid, depth) — no DOM
// measurement, no layout thrash — at the cost of sometimes spacing short-labelled nodes
// a little further apart than strictly necessary. That trade favors the hard constraint
// (pills must never overlap) over tighter packing.
const DEPTH_BOX: Record<0 | 1 | 2, NodeSize> = {
  0: { width: 320, height: 34 },
  1: { width: 168, height: 26 },
  2: { width: 116, height: 24 },
};

const COLLISION_PASSES = 64;
const COLLISION_GUTTER = 6;

export function estimateNodeBox(node: LayoutNode): NodeSize {
  return node.box ?? DEPTH_BOX[node.depth ?? 1];
}

// Whole-vault (global) view node sizing — GraphView's no-centre branch (globalGraph.ts /
// globalLayout.ts), added alongside the ego view's DEPTH_BOX above. 139 nodes has no room
// for the ego view's ~170px labelled pills, so global nodes are plain dots by default
// (DOT_BOX) with a permanent short label only for a small set of high-degree "hub" nodes
// (HUB_BOX, see globalGraph.ts's HUB_DEGREE_THRESHOLD) — kept in sync with graph.css's
// .global-node rules the same way DEPTH_BOX is kept in sync with .graph-node's.
export const DOT_BOX: NodeSize = { width: 14, height: 14 };
export const HUB_BOX: NodeSize = { width: 104, height: 22 };

/** Mulberry32 — small, deterministic, adequate for layout seeding. Exported so other
 * deterministic layouts (treeLayout.ts's radial forest) can reuse the same PRNG instead of
 * duplicating it, without needing the force simulation that lives in this file. */
export function rng(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = Math.imul(state ^ (state >>> 15), 1 | state);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const clamp = (value: number, low: number, high: number): number =>
  Math.min(Math.max(value, low), high);

export function layoutGraph(nodes: LayoutNode[], edges: GraphEdge[], box: Box): Map<string, Point> {
  const positions = new Map<string, Point>();
  if (nodes.length === 0) return positions;

  const random = rng(box.seed);
  const velocity = new Map<string, Point>();
  for (const node of nodes) {
    positions.set(node.uid, {
      x: MARGIN + random() * (box.width - 2 * MARGIN),
      y: MARGIN + random() * (box.height - 2 * MARGIN),
    });
    velocity.set(node.uid, { x: 0, y: 0 });
  }

  const linked = edges.filter((e) => positions.has(e.from_uid) && positions.has(e.to_uid));

  for (let step = 0; step < ITERATIONS; step++) {
    applyRepulsion(nodes, positions, velocity);
    applySprings(linked, positions, velocity);
    integrate(nodes, positions, velocity, box);
  }
  resolveCollisions(nodes, positions, box);
  return positions;
}

// Deterministic post-pass: nudges any pair of overlapping node boxes apart along
// whichever axis needs the smaller push, iterating in a fixed uid-sorted order for a
// bounded number of passes. Converges (or hits the pass cap) well before it matters for
// the graph sizes this view ever renders (<= MAX_EGO_NODES, see ego.ts).
//
// Exported so treeLayout.ts's radial forest layout can reuse it as a final cleanup pass
// (its own placement is geometric, not physics-based, but two leaves at the same ring and
// nearby angles can still land close enough to want this nudge) instead of re-implementing
// box separation.
export function resolveCollisions(nodes: LayoutNode[], positions: Map<string, Point>, box: Box): void {
  const ordered = [...nodes].sort((a, b) => a.uid.localeCompare(b.uid));

  for (let pass = 0; pass < COLLISION_PASSES; pass++) {
    let moved = false;
    for (let i = 0; i < ordered.length; i++) {
      const a = ordered[i];
      const boxA = estimateNodeBox(a);
      const pa = positions.get(a.uid)!;
      for (let j = i + 1; j < ordered.length; j++) {
        const b = ordered[j];
        const boxB = estimateNodeBox(b);
        const pb = positions.get(b.uid)!;
        if (separateOverlap(pa, boxA, pb, boxB, i, j)) moved = true;
      }
      clampToBox(pa, boxA, box);
    }
    if (!moved) break;
  }
}

// Pushes a and b apart in place (mutating their positions) if their boxes, centred on
// each point, overlap. Returns whether a push happened. When the two centres coincide
// (overlap is total, no natural direction), falls back to a fixed direction derived from
// (i, j) — deterministic, never NaN from a zero-length vector.
function separateOverlap(
  pa: Point,
  boxA: NodeSize,
  pb: Point,
  boxB: NodeSize,
  i: number,
  j: number,
): boolean {
  const minDx = (boxA.width + boxB.width) / 2 + COLLISION_GUTTER;
  const minDy = (boxA.height + boxB.height) / 2 + COLLISION_GUTTER;
  const rawDx = pb.x - pa.x;
  const rawDy = pb.y - pa.y;
  const coincident = rawDx === 0 && rawDy === 0;
  const dx = coincident ? ((j - i) % 2 === 0 ? 1 : -1) : rawDx;
  const dy = coincident ? 1 : rawDy;

  const overlapX = minDx - Math.abs(dx);
  const overlapY = minDy - Math.abs(dy);
  if (overlapX <= 0 || overlapY <= 0) return false;

  if (overlapX < overlapY) {
    const push = overlapX / 2 + 0.5;
    const sign = dx >= 0 ? 1 : -1;
    pa.x -= push * sign;
    pb.x += push * sign;
  } else {
    const push = overlapY / 2 + 0.5;
    const sign = dy >= 0 ? 1 : -1;
    pa.y -= push * sign;
    pb.y += push * sign;
  }
  return true;
}

function clampToBox(p: Point, size: NodeSize, box: Box): void {
  p.x = clamp(p.x, size.width / 2, box.width - size.width / 2);
  p.y = clamp(p.y, size.height / 2, box.height - size.height / 2);
}

function applyRepulsion(
  nodes: GraphNode[],
  positions: Map<string, Point>,
  velocity: Map<string, Point>,
): void {
  for (const a of nodes) {
    const pa = positions.get(a.uid)!;
    const va = velocity.get(a.uid)!;
    for (const b of nodes) {
      if (a.uid === b.uid) continue;
      const pb = positions.get(b.uid)!;
      const dx = pa.x - pb.x;
      const dy = pa.y - pb.y;
      const d2 = Math.max(dx * dx + dy * dy, 25);
      const force = REPULSION / d2;
      const d = Math.sqrt(d2);
      va.x += (dx / d) * force * 0.001;
      va.y += (dy / d) * force * 0.001;
    }
  }
}

function applySprings(
  linked: GraphEdge[],
  positions: Map<string, Point>,
  velocity: Map<string, Point>,
): void {
  for (const edge of linked) {
    const pa = positions.get(edge.from_uid)!;
    const pb = positions.get(edge.to_uid)!;
    const dx = pb.x - pa.x;
    const dy = pb.y - pa.y;
    const d = Math.max(Math.hypot(dx, dy), 1);
    const pull = (d - IDEAL) * SPRING;
    const va = velocity.get(edge.from_uid)!;
    const vb = velocity.get(edge.to_uid)!;
    va.x += (dx / d) * pull;
    va.y += (dy / d) * pull;
    vb.x -= (dx / d) * pull;
    vb.y -= (dy / d) * pull;
  }
}

function integrate(
  nodes: GraphNode[],
  positions: Map<string, Point>,
  velocity: Map<string, Point>,
  box: Box,
): void {
  for (const node of nodes) {
    const p = positions.get(node.uid)!;
    const v = velocity.get(node.uid)!;
    p.x = clamp(p.x + v.x, MARGIN, box.width - MARGIN);
    p.y = clamp(p.y + v.y, MARGIN, box.height - MARGIN);
    v.x *= DAMPING;
    v.y *= DAMPING;
  }
}


const GRID_GUTTER = 10;

export interface GridLayout {
  positions: Map<string, Point>;
  height: number;
}

/**
 * Deterministic uid-sorted row-major grid — no simulation, no randomness. Used for the
 * global view's isolated (degree-0) nodes (globalGraph.ts): a force layout has nothing to
 * pull a disconnected node toward, so applyRepulsion/applySprings would just leave it
 * wherever its random seed position happened to land — a plain grid is honest about that
 * instead of dressing up randomness as a "layout", and stays stable regardless of seed.
 *
 * Assumes a uniform box size across `nodes` (true for the dot-sized isolated nodes this is
 * built for): takes the first (uid-sorted) node's box as the cell size. `width` bounds the
 * row width the same way Box.width bounds layoutGraph; the grid's height is an output, not
 * an input, since a grid (unlike a force layout) has no free parameter to spend on filling
 * a taller box — it only ever needs as many rows as the node count requires.
 */
export function layoutGrid(nodes: LayoutNode[], width: number): GridLayout {
  const positions = new Map<string, Point>();
  const ordered = [...nodes].sort((a, b) => a.uid.localeCompare(b.uid));
  if (ordered.length === 0) return { positions, height: 0 };

  const cell = estimateNodeBox(ordered[0]);
  const cols = Math.max(1, Math.floor((width - 2 * MARGIN + GRID_GUTTER) / (cell.width + GRID_GUTTER)));
  const rows = Math.ceil(ordered.length / cols);

  ordered.forEach((node, i) => {
    const col = i % cols;
    const row = Math.floor(i / cols);
    positions.set(node.uid, {
      x: MARGIN + cell.width / 2 + col * (cell.width + GRID_GUTTER),
      y: MARGIN + cell.height / 2 + row * (cell.height + GRID_GUTTER),
    });
  });

  const height = 2 * MARGIN + rows * cell.height + (rows - 1) * GRID_GUTTER;
  return { positions, height };
}
