import cytoscape, { type Core, type ElementDefinition, type NodeSingular } from "cytoscape";
import type { PageRef } from "../../lib/types";
import fcose from "cytoscape-fcose";
import layoutUtilities from "cytoscape-layout-utilities";
import { useEffect, useRef, useState } from "react";
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
    // width for one 34px circle). False keeps the *layout* (node positions) compact; the
    // final fit-to-view (see cy.fit() in the mount effect below) separately and always
    // accounts for every label's true footprint regardless of this flag, so nothing painted
    // ever ends up outside the viewport — this flag only controls how tightly nodes are
    // allowed to sit next to each other, not what's guaranteed visible.
    nodeDimensionsIncludeLabels: false,
    tile: true,
    // Isolated (degree-0) nodes are tiled into one tidy block rather than scattered as
    // separate one-node components — tightened from fcose's own default (10/10) so that
    // block doesn't itself read as a second empty-looking region.
    tilingPaddingVertical: 4,
    tilingPaddingHorizontal: 4,
    packComponents: true,
    nestingFactor: 0.1,
    // Pulled tighter again after the fix round that followed this one: a zoom-floor used to
    // sit downstream of this layout and paper over an under-compressed result by zooming in
    // past whatever fit-to-content produced — which is exactly what let up to 40% of the
    // graph end up outside the viewport at 1024 wide. That mechanism is gone (see the mount
    // effect below); legibility now has to come entirely from how small a footprint fcose
    // settles on, so gravity/gravityCompound go up again and nodeSeparation/idealEdgeLength/
    // nodeRepulsion come down, on top of the round-1 values, to shrink both each scope's own
    // settled size and the total canvas fit-to-content has to zoom out to cover.
    gravity: 0.85,
    gravityCompound: 2.4,
    nodeSeparation: 26,
    idealEdgeLength: 30,
    // Above fcose's default (0.45) so the shorter idealEdgeLength above actually holds against
    // nodeRepulsion pushing connected nodes back apart — a soft ideal length with weak elastic
    // pull just lets repulsion win and the graph re-spreads back out.
    edgeElasticity: 0.6,
    // A function, not a flat number: turning off nodeDimensionsIncludeLabels (above) stopped
    // fcose reserving room for *every* node's label, which is what lets the graph pack tightly
    // — but it also stopped reserving room for the handful of labels that are always visible
    // (hubs and the ego centre, per graphStyle.ts's cascade). Giving only
    // `data("labelAlways")` nodes a bit of extra repulsion biases the initial placement toward
    // not crowding those labels together, reducing how often the deterministic label-collision
    // pass below (suppressOverlappingLabels) has to hide one to avoid an actual on-screen
    // overlap — it's a bias, not a guarantee, which is why that pass exists at all.
    nodeRepulsion: (node: NodeSingular) => (node.data("labelAlways") ? 6000 : 1800),
    randomize: true,
  };
}

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
    // Tightened from 24: componentSpacing is the gap layoutUtilities leaves *between* the
    // scope compounds (and the isolated-node tile) it packs — 24 of that was pure unused
    // margin between clusters that otherwise-tightened spacing (buildLayoutOptions) had no
    // way to touch.
    componentSpacing: 12,
  });
}

// includeNodes: false is load-bearing, not cosmetic. For a leaf node this trims the box down
// from "node circle + label" to just the label, which is all two *labels* colliding actually
// means. For a compound (.graph-scope) it matters far more: a compound's own bounding box IS
// the extent of all its descendants, so `includeLabels: true` alone (the default this used to
// call) returned the *entire scope cluster's footprint* as the scope's "label" box. Every hub
// label living inside that scope — which by definition sits inside its parent's full extent —
// then registered as overlapping that giant box and got suppressed, every time, for every
// scope: measured live, this was silently zeroing out all 33 nodes' labelAlways flags with
// none left visible at rest. `includeNodes: false` makes boundingBox() return just the
// rendered label rectangle in both cases (confirmed live: a scope title's box shrinks from
// the whole cluster down to ~39x19 model-space px at its actual text position).
function labelBox(ele: NodeSingular): { x1: number; y1: number; x2: number; y2: number } {
  return ele.boundingBox({ includeNodes: false, includeLabels: true, includeOverlays: false });
}

// The circle/shape a leaf node paints, deliberately excluding its label — used to keep a
// *different* node's label from ever printing on top of this node's body. A label rendered
// half-behind a neighbour's circle is just as broken as two labels overlapping each other
// (measured live: "Require dual ExpressRoute circuits..." with the middle third erased by a
// node drawn on top of it), so plain node bodies need to occupy the same "reserved" space as
// labels and scope titles do.
function nodeBodyBox(ele: NodeSingular): { x1: number; y1: number; x2: number; y2: number } {
  return ele.boundingBox({ includeNodes: true, includeLabels: false, includeOverlays: false });
}

function boxesOverlap(
  a: { x1: number; y1: number; x2: number; y2: number },
  b: { x1: number; y1: number; x2: number; y2: number },
): boolean {
  return a.x1 < b.x2 && b.x1 < a.x2 && a.y1 < b.y2 && b.y1 < a.y2;
}

// graphStyle.ts's label-density cascade keeps a node's label permanently on-screen whenever
// its `labelAlways` data flag is set (hubs, plus the ego centre and its immediate
// neighbours) — but that flag is assigned per-node, from degree/depth alone, by
// graphElements.ts, with no idea where fcose actually placed anything. Two qualifying nodes
// can still land close enough together that their labels print on top of each other, or a
// label can land on top of some unrelated node's circle — both were observed live even after
// the spacing tuning in buildLayoutOptions. Rather than trust a stochastic layout to always
// keep every permanently-visible label clear — "usually fine" isn't "never overlapping" —
// this walks every such label once positions settle and turns `labelAlways` back off for
// whichever member of an overlapping pair matters less, in priority order (the ego centre and
// depth<=1 neighbours first, then by degree). A suppressed label still appears on hover or
// once zoomed past HUB_ZOOM_THRESHOLD: graphStyle.ts's cascade reads the *current* value of
// the data flag, so flipping it at runtime here is enough. Scope titles and every node's own
// circle are never suppressed — reserved first, unconditionally — since an unlabelled cluster
// or a label-less node is a smaller loss than text nobody can read.
//
// Every reserved box is tagged with the id of the element it belongs to. That id is what
// keeps a node's *own* body from disqualifying its *own* label: a label sits immediately
// below its node (text-margin-y: 4) by design, so its box always touches or overlaps that
// same node's body box — that adjacency is normal layout, not a collision, and checking a
// hub's label against the full untagged reserved set (measured live) suppressed every single
// hub's label, unconditionally, before any cross-node check ever mattered. Only a *different*
// id's body/label counts as something to avoid.
interface ReservedBox {
  id: string;
  box: { x1: number; y1: number; x2: number; y2: number };
}

// cy.fit()'s bounding box defaults to includeLabels: true across every element it's given —
// and graphElements.ts gives *every* node a `label`, not just the handful graphStyle.ts's
// density cascade actually paints text for (scope titles, plus whichever nodes carry
// labelAlways). Measured live at 1024x768 on the real corpus: the label-inclusive box across
// all 33 nodes was 1667x965 model-space px versus 879x900 with labels excluded — cy.fit() was
// zooming out to reserve room for 32 labels nobody can see, which is what produced a
// technically-uncropped but needlessly small, mostly-empty-margin result (only ~1 hub label
// surviving suppressOverlappingLabels, the rest of the canvas blank). computeFitBox unions
// plain node/edge/compound geometry (every element, no labels) with the label geometry of only
// the elements the cascade actually renders unconditionally (scope titles, plus whatever
// labelAlways nodes survive suppressOverlappingLabels) — so the fit reserves exactly the space
// that will be painted, no more.
function unionBox(
  a: { x1: number; y1: number; x2: number; y2: number },
  b: { x1: number; y1: number; x2: number; y2: number },
): { x1: number; y1: number; x2: number; y2: number } {
  return {
    x1: Math.min(a.x1, b.x1),
    y1: Math.min(a.y1, b.y1),
    x2: Math.max(a.x2, b.x2),
    y2: Math.max(a.y2, b.y2),
  };
}

function computeFitBox(cy: Core): { x1: number; y1: number; x2: number; y2: number } {
  const bodies = cy
    .elements()
    .boundingBox({ includeLabels: false, includeNodes: true, includeOverlays: false });
  const visibleLabels = cy.elements(".graph-scope, .graph-node[?labelAlways]");
  if (visibleLabels.length === 0) return bodies;
  const labels = visibleLabels.boundingBox({
    includeLabels: true,
    includeNodes: false,
    includeOverlays: false,
  });
  return unionBox(bodies, labels);
}

// Replicates cy.fit()'s own zoom/pan arithmetic (zoom = min of width/height ratios against the
// padded container; pan centres the box) but against computeFitBox's box instead of cy.fit()'s
// default, label-inflated one — see computeFitBox's comment for why that substitution matters.
function fitToBox(cy: Core, padding: number): void {
  const box = computeFitBox(cy);
  const w = cy.width();
  const h = cy.height();
  const boxWidth = box.x2 - box.x1;
  const boxHeight = box.y2 - box.y1;
  if (!(boxWidth > 0) || !(boxHeight > 0) || !(w > 0) || !(h > 0)) {
    // Degenerate box (e.g. a single point, or a zero-size container mid-mount) — fall back to
    // Cytoscape's own fit rather than divide by zero.
    cy.fit(cy.elements(), padding);
    return;
  }
  const zoom = Math.min((w - 2 * padding) / boxWidth, (h - 2 * padding) / boxHeight);
  cy.zoom(zoom);
  cy.pan({
    x: (w - zoom * (box.x1 + box.x2)) / 2,
    y: (h - zoom * (box.y1 + box.y2)) / 2,
  });
}

// graphStyle.ts's label font-size is a function of `cy.zoom()` (a hand-built floor, since
// Cytoscape's min-zoomed-font-size hides rather than floors — see the comment on
// zoomCompensatedFontSize) so that on-screen label size stays legible regardless of how far
// fit-to-content had to zoom out. That creates a small circularity: fitToBox's bounding box
// includes label geometry, but that geometry's *model-space* extent depends on the current
// zoom, which fitToBox is about to change. One pass can therefore land a hair short of
// self-consistent — this repeats fit -> style().update() a few times so the bounding box used
// by the final fit reflects label sizes measured at that same fit's own zoom, which is the
// coordinator's own "fit again after the layout settles" guidance applied to a zoom-dependent
// style rather than only to fcose's stochastic node positions. Cheap at this graph's scale
// (tens of nodes), and each pass is a plain synchronous call. A final fitToBox runs after
// suppressOverlappingLabels, since suppression can only turn labelAlways *off* — never on —
// so the fit box computed post-suppression is the same size or smaller, never bigger, meaning
// this last pass can only tighten the view further, never re-introduce cropping.
const FIT_SETTLE_PASSES = 2;

function settleFit(cy: Core): void {
  for (let pass = 0; pass < FIT_SETTLE_PASSES; pass += 1) {
    fitToBox(cy, FIT_PADDING);
    cy.style().update();
  }
  suppressOverlappingLabels(cy);
  fitToBox(cy, FIT_PADDING);
}

function suppressOverlappingLabels(cy: Core): void {
  const reserved: ReservedBox[] = [
    ...cy.nodes(".graph-scope").map((scope) => ({ id: scope.id(), box: labelBox(scope) })),
    ...cy.nodes(".graph-node").map((node) => ({ id: node.id(), box: nodeBodyBox(node) })),
  ];

  const priority = (node: NodeSingular): number => {
    if (node.data("isCentre")) return Number.POSITIVE_INFINITY;
    const depth = node.data("depth");
    const depthBonus = typeof depth === "number" && depth <= 1 ? 1_000_000 : 0;
    return depthBonus + (Number(node.data("degree")) || 0);
  };

  const hubs = cy
    .nodes(".graph-node[?labelAlways]")
    .sort((a, b) => priority(b) - priority(a) || (a.id() < b.id() ? -1 : 1));

  hubs.forEach((node) => {
    const id = node.id();
    const box = labelBox(node);
    const collides = reserved.some((other) => other.id !== id && boxesOverlap(box, other.box));
    if (collides) {
      node.data("labelAlways", false);
      return;
    }
    reserved.push({ id, box });
  });
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
      // Label font-size is zoom-dependent (graphStyle.ts's zoomCompensatedFontSize) so that
      // its on-screen size stays constant instead of shrinking — or vanishing outright, per
      // min-zoomed-font-size's real hide-below-threshold behaviour — as zoom drops. Cytoscape
      // does not re-invoke function-valued styles on every zoom/pan tick on its own, so this
      // forces the recompute on every wheel-zoom, pinch, drag-pan, or button click.
      cy.style().update();
    };
    cy.on("zoom pan", syncZoomClass);
    syncZoomClass();
    // fcose's own `fit: true` runs once the layout (and, unless reduced-motion, its 400ms
    // settle animation) finishes, but compound (scope) box geometry can still be settling
    // relative to when fcose computed that internal fit — so `layoutstop` re-fits explicitly,
    // synchronously, with no animation, as the actual guarantee that nothing is cropped. This
    // calls settleFit (repeated fitToBox + style().update() to converge with the zoom-dependent
    // label font-size, see settleFit's comment) rather than a plain cy.fit(): fitToBox's box
    // always includes every element's plain node/edge geometry (nothing paintable can ever end
    // up outside it) unioned with the label geometry of whatever is actually going to be shown
    // text for, so nothing painted ever ends up outside the viewport without reserving space for
    // labels nobody will see. Label collisions are a separate concern from cropping, handled by
    // suppressOverlappingLabels inside settleFit.
    cy.on("layoutstop", () => settleFit(cy as Core));

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
      // Deliberately not cy.animate({ fit: {...} }): that shorthand computes its target zoom
      ///pan from cy.fit()'s own default (label-inclusive-for-every-element) bounding box, which
      // would animate toward the same over-zoomed-out point computeFitBox exists to avoid (see
      // its comment) before `complete` snapped back to the tighter, correct result — a visible
      // zoom-out-then-jump-back. Animating directly to the zoom/pan computeFitBox already
      // prescribes avoids that. Node positions don't change here, only zoom/pan — but label
      // font-size tracks zoom (graphStyle.ts's zoomCompensatedFontSize), so this target can end
      // up a hair off from what that same zoom would produce once labels resize to match it.
      // `complete` runs settleFit once the animation lands, repeating fitToBox + style().update()
      // (and re-running suppressOverlappingLabels) to converge on a self-consistent,
      // guaranteed-uncropped result exactly as layoutstop does after a re-layout.
      const box = computeFitBox(cy);
      const w = cy.width();
      const h = cy.height();
      const boxWidth = box.x2 - box.x1;
      const boxHeight = box.y2 - box.y1;
      const canComputeTarget = boxWidth > 0 && boxHeight > 0 && w > 0 && h > 0;
      const zoom = canComputeTarget
        ? Math.min((w - 2 * FIT_PADDING) / boxWidth, (h - 2 * FIT_PADDING) / boxHeight)
        : cy.zoom();
      const pan = canComputeTarget
        ? { x: (w - zoom * (box.x1 + box.x2)) / 2, y: (h - zoom * (box.y1 + box.y2)) / 2 }
        : cy.pan();
      cy.animate({
        zoom,
        pan,
        duration: prefersReducedMotion() ? 0 : 200,
        complete: () => settleFit(cy),
      });
    },
  };
}
