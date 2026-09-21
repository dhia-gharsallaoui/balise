import { stripBackticks } from "../../lib/richTitle";
import type { GraphNode, PageRef } from "../../lib/types";
import type { Point } from "./layout";

// Renders one scope panel's nodes for the whole-vault global graph (GraphView's no-centre
// branch — see globalGraph.ts / globalLayout.ts for how a panel's nodes and positions are
// built). 139 nodes at once has no room for the ego view's always-labelled pills
// (GraphNodes.tsx): every node is a small dot, coloured by type with the same --type-*
// tokens/CSS pattern GraphNodes.tsx uses. The full title is always reachable (aria-label +
// native title tooltip); a visible text label shows on hover/focus for any node, and
// permanently for a small set of high-degree "hub" nodes (hubUids — see
// HUB_DEGREE_THRESHOLD in globalGraph.ts), which read as landmarks the way the ego view's
// depth-0 centre does. Isolated (degree-0) nodes render as a hollow ring instead of a
// filled dot (graph.css's [data-isolated="true"]) — a visible fact about the page, not a
// muted/dimmed afterthought.

export function GlobalGraphNodes({
  nodes,
  positions,
  width,
  height,
  hubUids,
  hovered,
  neighbours,
  onHover,
  onOpen,
  isolated = false,
}: {
  nodes: GraphNode[];
  positions: Map<string, Point>;
  width: number;
  height: number;
  hubUids: Set<string>;
  hovered: string | null;
  neighbours: Set<string>;
  onHover: (uid: string | null) => void;
  onOpen: (ref: PageRef) => void;
  isolated?: boolean;
}) {
  return (
    <>
      {nodes.map((node) => {
        const point = positions.get(node.uid);
        if (!point) return null;
        const dim = hovered !== null && hovered !== node.uid && !neighbours.has(node.uid);
        const isHub = hubUids.has(node.uid);
        return (
          <button
            key={node.uid}
            type="button"
            className="global-node"
            data-type={node.type}
            data-hub={isHub ? "true" : "false"}
            data-isolated={isolated ? "true" : "false"}
            data-dim={dim ? "true" : "false"}
            aria-label={node.title}
            title={node.title}
            style={{ left: `${(point.x / width) * 100}%`, top: `${(point.y / height) * 100}%` }}
            onMouseEnter={() => onHover(node.uid)}
            onMouseLeave={() => onHover(null)}
            onFocus={() => onHover(node.uid)}
            onBlur={() => onHover(null)}
            onClick={() => onOpen({ scope: node.scope, slug: node.slug })}
          >
            <span className="global-node-dot" data-type={node.type} aria-hidden="true" />
            <span className="global-node-label">{stripBackticks(node.title)}</span>
          </button>
        );
      })}
    </>
  );
}
