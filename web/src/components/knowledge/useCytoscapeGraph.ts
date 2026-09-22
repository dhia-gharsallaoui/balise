import cytoscape, { type Core, type ElementDefinition } from "cytoscape";
import type { PageRef } from "../../lib/types";
import fcose from "cytoscape-fcose";
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

let fcoseRegistered = false;
function ensureFcoseRegistered(): void {
  if (fcoseRegistered) return;
  cytoscape.use(fcose);
  fcoseRegistered = true;
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
  packComponents: boolean;
  nestingFactor: number;
  gravity: number;
  gravityCompound: number;
  idealEdgeLength: number;
  edgeElasticity: number;
  nodeRepulsion: number;
  randomize: boolean;
}

function buildLayoutOptions(): FcoseLayoutOptions {
  const reducedMotion = prefersReducedMotion();
  return {
    name: "fcose",
    quality: "default",
    animate: !reducedMotion,
    animationDuration: 400,
    fit: true,
    padding: 48,
    nodeDimensionsIncludeLabels: true,
    tile: true,
    packComponents: true,
    nestingFactor: 0.1,
    gravity: 0.3,
    gravityCompound: 1.2,
    idealEdgeLength: 80,
    edgeElasticity: 0.45,
    nodeRepulsion: 6000,
    randomize: true,
  };
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
      ensureFcoseRegistered();
      cy = cytoscape({
        container,
        elements,
        style: buildGraphStylesheet(readGraphPalette()),
        layout: buildLayoutOptions() as unknown as cytoscape.LayoutOptions,
        wheelSensitivity: 0.2,
      });
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
        fit: { eles: cy.elements(), padding: 48 },
        duration: prefersReducedMotion() ? 0 : 200,
      });
    },
  };
}
