import { useRef } from "react";
import { ArrowsOut, Minus, Plus } from "@phosphor-icons/react";
import type { ElementDefinition } from "cytoscape";
import type { PageRef } from "../../lib/types";
import type { EgoGraph } from "./ego";
import { GraphA11yList } from "./GraphA11yList";
import type { GlobalGraph } from "./globalGraph";
import { useCytoscapeGraph } from "./useCytoscapeGraph";

// The shared shell for both graph modes: a Cytoscape canvas ("go in and out" — pan/zoom,
// with on-screen controls plus a fit-to-content reset, since scroll-to-zoom inside a
// scrolling page is easy to trigger by accident) and, as a plain DOM sibling, the
// accessible list that stands in for it on a screen reader or a keyboard-only pass.
//
// `role="img"` on `.cy-container` collapses the canvas subtree into one atomic accessible
// object instead of exposing Cytoscape's internal (non-semantic) canvas layers; the real
// per-node accessible content lives in `.graph-a11y-list` right beside it.

interface GraphCanvasProps {
  mode: "ego" | "global";
  elements: ElementDefinition[];
  nodeCount: number;
  ego?: EgoGraph;
  global?: GlobalGraph;
  collapsedScopes?: ReadonlySet<string>;
  onOpen: (ref: PageRef) => void;
  onToggleScope?: (scope: string) => void;
}

export function GraphCanvas({
  mode,
  elements,
  nodeCount,
  ego,
  global,
  collapsedScopes,
  onOpen,
  onToggleScope,
}: GraphCanvasProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const stageRef = useRef<HTMLDivElement | null>(null);

  const { zoomIn, zoomOut, fitToContent } = useCytoscapeGraph({
    containerRef,
    wheelGuardRef: stageRef,
    elements,
    onOpenNode: onOpen,
    onToggleScope,
  });

  return (
    <div className="graph-stage" ref={stageRef}>
      <div
        className="cy-container"
        ref={containerRef}
        role="img"
        aria-label={`Relation graph of ${nodeCount} pages`}
      />
      <div className="graph-zoom-controls" role="group" aria-label="Graph zoom">
        <button type="button" aria-label="Zoom in" title="Zoom in" onClick={zoomIn}>
          <Plus size={14} aria-hidden="true" />
        </button>
        <button type="button" aria-label="Zoom out" title="Zoom out" onClick={zoomOut}>
          <Minus size={14} aria-hidden="true" />
        </button>
        <button type="button" aria-label="Fit graph to view" title="Fit graph to view" onClick={fitToContent}>
          <ArrowsOut size={14} aria-hidden="true" />
        </button>
      </div>
      <p className="graph-hint-scroll">
        Hold Ctrl (or ⌘) and scroll, or pinch, to zoom. Click a node to open it.
      </p>
      <GraphA11yList
        mode={mode}
        ego={ego}
        global={global}
        collapsedScopes={collapsedScopes}
        onOpen={onOpen}
        onToggleScope={onToggleScope}
      />
    </div>
  );
}
