import type { GraphEdge } from "../../lib/types";
import type { Point } from "./layout";

// Renders the ego-graph's edges as an <svg>. Stroke colour and dash pattern come from
// data-role, styled in graph.css from --role-* tokens: dashed for the untyped "link"
// kind, solid for every typed relation (spec §6.7 F-61).

export function GraphEdges({
  edges,
  positions,
  width,
  height,
  nodeCount,
  hovered,
  scopeLabel,
  primaryEdgeIndices,
}: {
  edges: GraphEdge[];
  positions: Map<string, Point>;
  width: number;
  height: number;
  nodeCount: number;
  hovered: string | null;
  // Global view only: several of these SVGs render at once (one per scope panel), so each
  // needs a distinguishable accessible name. Omitted, the label matches the ego view's
  // original text exactly (unchanged for that path).
  scopeLabel?: string;
  // Global view only (fix round): indices into `edges` that are the panel's spanning-tree
  // edges (treeLayout.ts). When given, every other index renders with data-secondary="true"
  // (graph.css dims and thins it) so a dense panel's ~2/3 tree edges read as the primary
  // structure and the rest read as cross-links, not uniform noise. Omitted entirely (the
  // ego view never passes it), every edge keeps its original single-weight styling.
  primaryEdgeIndices?: Set<number>;
}) {
  const label = scopeLabel
    ? `Relation graph of ${scopeLabel} scope, ${nodeCount} pages`
    : `Relation graph of ${nodeCount} pages`;
  return (
    <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label}>
      {edges.map((edge, index) => {
        const from = positions.get(edge.from_uid);
        const to = positions.get(edge.to_uid);
        if (!from || !to) return null;
        const dim = hovered !== null && edge.from_uid !== hovered && edge.to_uid !== hovered;
        const secondary = primaryEdgeIndices ? !primaryEdgeIndices.has(index) : false;
        return (
          <line
            key={`${edge.from_uid}-${edge.to_uid}-${index}`}
            x1={from.x}
            y1={from.y}
            x2={to.x}
            y2={to.y}
            className="graph-edge"
            data-role={edge.kind}
            data-dim={dim ? "true" : "false"}
            data-secondary={secondary ? "true" : undefined}
          />
        );
      })}
    </svg>
  );
}
