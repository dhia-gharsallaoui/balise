import { HUB_DEGREE_THRESHOLD, type ScopePanel } from "./globalGraph";
import { DOT_BOX, HUB_BOX, layoutGrid, type LayoutNode, type Point } from "./layout";
import { layoutRadialForest } from "./treeLayout";

// Composes one scope panel's geometry for the global view: the connected nodes get the
// radial spanning-tree layout (treeLayout.ts — fix round: replaces the general-purpose
// force layout for this view after Work/Client-Globex's density review showed an under-
// converged simulation, not a data problem, see treeLayout.ts's header), and the isolated
// nodes get the deterministic grid "shelf" (layoutGrid), kept as its own separate canvas
// rather than merged into one coordinate space, so the two regions never visually blend.

const PANEL_WIDTH = 480;
// A handful of the largest scopes (currently Work at 65 connected nodes, Client-Globex at 41)
// get a wider canvas in addition to a taller one (see connectedHeightFor): more room in
// both axes for the radial tree to fan out into, matching graph.css's xlarge flex-basis so
// the SVG's internal coordinate units still track rendered pixels roughly 1:1 (a node's
// on-screen size stays constant across tiers; only the space between nodes grows).
const XLARGE_PANEL_WIDTH = 640;
const LARGE_THRESHOLD = 20;
const XLARGE_THRESHOLD = 35;

const MIN_CONNECTED_HEIGHT = 160;
// Raised from the fix round's original 520px cap, which squeezed Work's 65 nodes into the
// same vertical budget as a 12-node panel — "Panel height should scale with node count" was
// the direct ask. 1200 is a generous ceiling for a future much-larger scope, not a value
// any current panel reaches (Work computes to ~710px).
const MAX_CONNECTED_HEIGHT = 1200;
const HEIGHT_PER_NODE = 10;

export interface PanelLayout {
  scope: string;
  width: number;
  connectedHeight: number;
  connectedPositions: Map<string, Point>;
  // Indices into `panel.edges` that are spanning-tree edges (treeLayout.ts) — every other
  // index is a cross-link. GlobalGraph.tsx passes this straight through to GraphEdges so
  // cross-links render as visually secondary (thinner/fainter), giving the two-thirds of
  // Work's edges that are tree edges clear visual priority over the rest.
  treeEdgeIndices: Set<number>;
  isolatedHeight: number;
  isolatedPositions: Map<string, Point>;
  // uids of this panel's connected nodes at or above HUB_DEGREE_THRESHOLD — rendered as
  // permanently-labelled landmarks instead of plain dots (GlobalGraphNodes.tsx).
  hubUids: Set<string>;
}

export type PanelSizeClass = "small" | "large" | "xlarge";

/** Single source of truth for the small/large/xlarge tiering, shared between this file's
 * own width choice and GlobalGraph.tsx's `data-size` attribute (graph.css's flex-basis
 * rules) — kept in one place so the two can never drift apart. */
export function panelSizeClass(total: number): PanelSizeClass {
  if (total >= XLARGE_THRESHOLD) return "xlarge";
  if (total >= LARGE_THRESHOLD) return "large";
  return "small";
}

function widthFor(total: number): number {
  return panelSizeClass(total) === "xlarge" ? XLARGE_PANEL_WIDTH : PANEL_WIDTH;
}

function connectedHeightFor(count: number): number {
  return Math.min(MAX_CONNECTED_HEIGHT, Math.max(MIN_CONNECTED_HEIGHT, 60 + count * HEIGHT_PER_NODE));
}

export function layoutScopePanel(panel: ScopePanel, seed: number): PanelLayout {
  const total = panel.connected.length + panel.isolated.length;
  const width = widthFor(total);

  const hubUids = new Set(
    panel.connected
      .filter((n) => (panel.degree.get(n.uid) ?? 0) >= HUB_DEGREE_THRESHOLD)
      .map((n) => n.uid),
  );

  const connectedNodes: LayoutNode[] = panel.connected.map((n) => ({
    ...n,
    box: hubUids.has(n.uid) ? HUB_BOX : DOT_BOX,
  }));
  const connectedHeight = connectedHeightFor(panel.connected.length);
  const { positions: connectedPositions, treeEdgeIndices } = layoutRadialForest(
    connectedNodes,
    panel.edges,
    { width, height: connectedHeight, seed },
  );

  const isolatedNodes: LayoutNode[] = panel.isolated.map((n) => ({ ...n, box: DOT_BOX }));
  const { positions: isolatedPositions, height: isolatedHeight } = layoutGrid(isolatedNodes, width);

  return {
    scope: panel.scope,
    width,
    connectedHeight,
    connectedPositions,
    treeEdgeIndices,
    isolatedHeight,
    isolatedPositions,
    hubUids,
  };
}

/** One panel layout per scope, in the same order as `panels` (already deterministic — see
 * buildGlobalGraph). Each panel's own layout call is independent of every other panel's, by
 * construction (separate node/edge/box arrays and separate width/height), so this never
 * lets one scope's tree/grid layout see or move another's nodes. */
export function layoutGlobalGraph(panels: ScopePanel[], seed: number): PanelLayout[] {
  return panels.map((panel) => layoutScopePanel(panel, seed));
}
