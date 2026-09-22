import cytoscape, { type Core, type ElementDefinition, type NodeSingular } from "cytoscape";
import type { PageRef } from "../../lib/types";
import fcose from "cytoscape-fcose";
import layoutUtilities from "cytoscape-layout-utilities";
import { useEffect, useRef, useState } from "react";
import { MIN_NODE_SIZE } from "./graphElements";
import { buildGraphStylesheet, HUB_ZOOM_THRESHOLD, readGraphPalette, watchThemeChange } from "./graphStyle";

// Owns the one thing graphElements.ts and graphStyle.ts deliberately know nothing about:
// an actual mounted Cytoscape instance. Kept as a hook (rather than folding this into
// GraphCanvas.tsx) so the DOM-and-canvas half of the graph is unit-testable in isolation
// from the JSX that renders the accessible list beside it — though in practice the only
// thing unit tests exercise here is that it degrades gracefully when canvas doesn't exist
// (jsdom), never actual Cytoscape rendering/interaction, which is Playwright's job.

interface UseCytoscapeGraphOptions {
  containerRef: React.RefObject<HTMLDivElement | null>;
  // An ancestor of containerRef. A capture-phase wheel listener here swallows plain wheel
  // scrolls before they ever reach Cytoscape's own canvas listener, so the page scrolls
  // normally by default; holding Ctrl/Cmd (or a trackpad pinch, which reports ctrlKey=true)
  // lets the event through to Cytoscape's native zoom-on-wheel behaviour.
  wheelGuardRef?: React.RefObject<HTMLElement | null>;
  elements: ElementDefinition[];
  onOpenNode: (ref: PageRef) => void;
  onToggleScope?: (scope: string) => void;
}

interface UseCytoscapeGraphHandle {
  ready: boolean;
  zoomIn: () => void;
  zoomOut: () => void;
  fitToContent: () => void;
}

let layoutExtensionsRegistered = false;
// fcose's `packComponents: true` option (buildLayoutOptions below) is a documented no-op
// unless cytoscape-layout-utilities is also registered — see the .d.ts shim for that module.
// Without it, the 4 top-level scope compounds (which never share an edge, by
// globalGraph.ts's design) and every isolated single-node "component" all go through fcose's
// force layout as one undifferentiated pass, with nothing to pull the disconnected pieces
// back together afterwards — which is what left fit-to-content zoomed out far enough that
// nodes and labels shrank to illegible sizes. Registering both extensions together is what
// makes packComponents actually pack.
function ensureLayoutExtensionsRegistered(): void {
  if (layoutExtensionsRegistered) return;
  cytoscape.use(fcose);
  cytoscape.use(layoutUtilities);
  layoutExtensionsRegistered = true;
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

interface FcoseLayoutOptions {
  name: "fcose";
  quality: "default" | "draft" | "proof";
  animate: boolean;
  animationDuration: number;
  fit: boolean;
  padding: number;
  nodeDimensionsIncludeLabels: boolean;
  tile: boolean;
  tilingPaddingVertical: number;
  tilingPaddingHorizontal: number;
  packComponents: boolean;
  nestingFactor: number;
  gravity: number;
  gravityCompound: number;
  nodeSeparation: number;
  idealEdgeLength: number;
  edgeElasticity: number;
  nodeRepulsion: (node: NodeSingular) => number;
  randomize: boolean;
}

// Fit-to-content padding shared by the initial layout and the manual "fit to view" button —
// kept small (vs. the old 48px) so the fitted view spends its space on content, not margin.
const FIT_PADDING = 28;

function buildLayoutOptions(): FcoseLayoutOptions {
  const reducedMotion = prefersReducedMotion();
  return {
    name: "fcose",
    quality: "default",
    animate: !reducedMotion,
    animationDuration: 400,
    fit: true,
    padding: FIT_PADDING,
    // Most labels are hidden at rest (graphStyle.ts's density cascade: only hubs/centre show
    // a label until you zoom past HUB_ZOOM_THRESHOLD) — but fcose still reserves each node's
    // *full label text width* as layout footprint when this is true, regardless of whether
    // that label is actually painted. Measured live: with this on, the packed graph's
    // bounding box was 3-4x larger than the container needed for legible-zoom fit (a title
    // like "Recreating an AKS node pool drops custom taints" reserves ~250 model-space px of
    // width for one 34px circle). False trades a little more label crowding once you zoom in
    // close (where there's room to pan around it) for a graph that actually fits and reads at
    // rest, which is what was asked for.
    nodeDimensionsIncludeLabels: false,
    tile: true,
    // Isolated (degree-0) nodes are tiled into one tidy block rather than scattered as
    // separate one-node components — tightened from fcose's own default (10/10) so that
    // block doesn't itself read as a second empty-looking region.
    tilingPaddingVertical: 6,
    tilingPaddingHorizontal: 6,
    packComponents: true,
    nestingFactor: 0.1,
    // Below fcose's own defaults (gravity 0.25, gravityCompound 1.0) on purpose: the previous
    // values (0.3 / 1.2) plus an above-default nodeRepulsion/idealEdgeLength were spreading
    // each scope's members out more than necessary. Pulling harder (gravity, gravityCompound)
    // while pushing less (nodeRepulsion, idealEdgeLength) shrinks each scope's own settled
    // size; packComponents (now functional — see ensureLayoutExtensionsRegistered above) is
    // what keeps the 4 scopes themselves, and the isolated-node tile, close together.
    gravity: 0.45,
    gravityCompound: 1.6,
    nodeSeparation: 45,
    idealEdgeLength: 42,
    edgeElasticity: 0.45,
    // A function, not a flat number: turning off nodeDimensionsIncludeLabels (above) stopped
    // fcose reserving room for *every* node's label, which is what let the graph pack tightly
    // — but it also stopped reserving room for the handful of labels that are always visible
    // (hubs and the ego centre, per graphStyle.ts's cascade), and those were measured
    // overlapping each other at rest once the graph tightened up. Giving only
    // `data("labelAlways")` nodes extra repulsion pushes the always-labelled few further
    // apart from their neighbours without re-inflating spacing around the majority of nodes,
    // whose labels stay hidden until zoomed in anyway.
    nodeRepulsion: (node: NodeSingular) => (node.data("labelAlways") ? 14000 : 2600),
    randomize: true,
  };
}

// However tightly fcose packs things, fit-to-content can still land on a low zoom for a
// large or spread-out graph — and a low zoom shrinks node/label pixel size right along with
// it, since both are defined in Cytoscape's model-space units. This is the backstop the
// layout/packing tuning above can't fully guarantee on its own: after any auto-fit (initial
// layout, or the "fit to view" button), if the resulting zoom would render even the smallest
// node under MIN_NODE_SCREEN_PX on screen, zoom back in toward that floor instead of leaving
// it that small. Trades "everything visible at once" for "what's visible is legible" —
// panning covers the rest, same as before.
const MIN_NODE_SCREEN_PX = 26;

// Bound on how much clampZoomFloor is allowed to zoom in past the natural fit: raising zoom
// with no regard for how large the content actually is can leave most of the graph outside
// the viewport entirely (proven live during the fix round — forcing zoom up on an untamed
// layout took a "half-filled canvas" complaint to "almost nothing visible"). Never zoom in
// far enough that less than this fraction of the content's bounding box, on either axis,
// would remain in view — legible-but-mostly-off-screen is worse than the size floor missed.
const MIN_VISIBLE_FRACTION = 0.6;

// cytoscape-layout-utilities defaults to packing components toward a square (1:1) arrangement,
// which fights a landscape container: a wide, short canvas is left with unused width if the
// packed content is roughly as tall as it is wide (this is exactly what left a chunk of the
// tallest scope cut off above the container's edge during the fix round — packing was
// tightening the layout, but into the wrong aspect ratio for where it had to fit). Matching
// the packer's target aspect ratio to the container's actual aspect ratio, and tightening its
// default 80px inter-component gap to match the smaller gaps used elsewhere in this layout
// (tilingPaddingVertical/Horizontal above), lets fit-to-content use the space it has instead
// of the space a square packing would need.
function configureComponentPacking(cy: Core): void {
  const width = cy.width();
  const height = cy.height();
  cy.layoutUtilities({
    desiredAspectRatio: width > 0 && height > 0 ? width / height : 1,
    // Measured live: utilityFunction 2 (blend fullness + aspect match) was tried and
    // reverted — it hit the target aspect ratio more closely but did so by leaving far more
    // empty space inside the packed bounding box (component footprint grew ~75% versus
    // utilityFunction 1 on the same graph), which is a worse trade for "fill the canvas"
    // than the small aspect-ratio mismatch left over from the default fullness-first
    // utility. Left at the default (1) deliberately.
    componentSpacing: 24,
  });
}

function clampZoomFloor(cy: Core): void {
  const floorZoom = MIN_NODE_SCREEN_PX / MIN_NODE_SIZE;
  const currentZoom = cy.zoom();
  if (currentZoom >= floorZoom) return;

  const bb = cy.elements().boundingBox();
  const safeZoomCap =
    bb.w > 0 && bb.h > 0
      ? Math.min(cy.width() / (bb.w * MIN_VISIBLE_FRACTION), cy.height() / (bb.h * MIN_VISIBLE_FRACTION))
      : floorZoom;
  const targetZoom = Math.max(currentZoom, Math.min(floorZoom, safeZoomCap));
  if (targetZoom <= currentZoom) return;

  cy.zoom({ level: targetZoom, renderedPosition: { x: cy.width() / 2, y: cy.height() / 2 } });
  cy.center(cy.elements());
}

export function useCytoscapeGraph(options: UseCytoscapeGraphOptions): UseCytoscapeGraphHandle {
  const { containerRef, wheelGuardRef, elements, onOpenNode, onToggleScope } = options;
  const cyRef = useRef<Core | null>(null);
  const [ready, setReady] = useState(false);
  const onOpenNodeRef = useRef(onOpenNode);
  onOpenNodeRef.current = onOpenNode;
  const onToggleScopeRef = useRef(onToggleScope);
  onToggleScopeRef.current = onToggleScope;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let cy: Core | null = null;
    try {
      ensureLayoutExtensionsRegistered();
      cy = cytoscape({
        container,
        elements,
        style: buildGraphStylesheet(readGraphPalette()),
        wheelSensitivity: 0.2,
      });
      // Must run after the core exists (needs cy.width()/height()) but before the first
      // layout — fcose fetches whatever layoutUtilities instance is already configured on
      // this cy ("get", no options) rather than creating a default-options one of its own.
      configureComponentPacking(cy);
      cy.layout(buildLayoutOptions() as unknown as cytoscape.LayoutOptions).run();
    } catch {
      // No 2D canvas context (jsdom in unit tests) or another mounting failure — the
      // accessible list is still fully functional, so this is a degradation, not a crash.
      setReady(false);
      return;
    }

    cyRef.current = cy;
    setReady(true);

    cy.on("tap", "node.graph-node", (evt) => {
      const scope = evt.target.data("scope");
      const slug = evt.target.data("slug");
      if (typeof scope === "string" && typeof slug === "string") {
        onOpenNodeRef.current({ scope, slug });
      }
    });
    cy.on("tap", "node.graph-scope", (evt) => {
      const scope = evt.target.data("scope");
      if (typeof scope === "string") onToggleScopeRef.current?.(scope);
    });
    // Hovering a node highlights its immediate neighbourhood (itself + connected edges +
    // adjacent nodes) and dims everything else — this is what makes "see connection and
    // nodes clear" hold up once a scope has 30+ nodes on screen: the cascade in
    // graphStyle.ts already fades unrelated elements via .graph-node-dim/.graph-edge-dim,
    // this just decides which elements those classes land on for the current hover.
    cy.on("mouseover", "node.graph-node", (evt) => {
      const node = evt.target;
      const neighbourhood = node.closedNeighborhood();
      cy.nodes(".graph-node").difference(neighbourhood).addClass("graph-node-dim");
      cy.edges(".graph-edge").difference(neighbourhood).addClass("graph-edge-dim");
      node.addClass("graph-node-hover");
      node.connectedEdges().addClass("graph-edge-hover");
    });
    cy.on("mouseout", "node.graph-node", () => {
      cy.nodes(".graph-node").removeClass("graph-node-dim graph-node-hover");
      cy.edges(".graph-edge").removeClass("graph-edge-dim graph-edge-hover");
    });
    const syncZoomClass = () => {
      if (!cy) return;
      cy.nodes(".graph-node").toggleClass("zoomed-in", cy.zoom() >= HUB_ZOOM_THRESHOLD);
    };
    cy.on("zoom pan", syncZoomClass);
    syncZoomClass();
    // fcose's own `fit: true` runs once the layout (and, unless reduced-motion, its 400ms
    // settle animation) finishes — `layoutstop` fires after that, so this is the first safe
    // point to check whether the fit it chose left nodes too small and correct it.
    cy.on("layoutstop", () => clampZoomFloor(cy));

    let resizeObserver: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(() => cy?.resize());
      resizeObserver.observe(container);
    }

    const wheelGuard = wheelGuardRef?.current;
    const onWheel = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) event.stopPropagation();
    };
    wheelGuard?.addEventListener("wheel", onWheel, { capture: true, passive: true });

    return () => {
      resizeObserver?.disconnect();
      wheelGuard?.removeEventListener("wheel", onWheel, { capture: true });
      cy?.destroy();
      cyRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [elements]);

  useEffect(() => {
    return watchThemeChange(() => {
      cyRef.current?.style(buildGraphStylesheet(readGraphPalette()));
    });
  }, []);

  return {
    ready,
    zoomIn: () => {
      const cy = cyRef.current;
      if (!cy) return;
      cy.animate({ zoom: cy.zoom() * 1.3, duration: prefersReducedMotion() ? 0 : 150 });
    },
    zoomOut: () => {
      const cy = cyRef.current;
      if (!cy) return;
      cy.animate({ zoom: cy.zoom() / 1.3, duration: prefersReducedMotion() ? 0 : 150 });
    },
    fitToContent: () => {
      const cy = cyRef.current;
      if (!cy) return;
      cy.animate({
        fit: { eles: cy.elements(), padding: FIT_PADDING },
        duration: prefersReducedMotion() ? 0 : 200,
        complete: () => clampZoomFloor(cy),
      });
    },
  };
}
