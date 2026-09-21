import { stripBackticks } from "../../lib/richTitle";
import type { PageRef } from "../../lib/types";
import type { EgoNode } from "./ego";
import type { Point } from "./layout";

// Renders the ego-graph's nodes as buttons positioned over the <svg>. Sizing/opacity by
// data-depth (graph.css) gives the visual hierarchy the spec asks for: the centre reads
// as a headline and anchors the view, depth-2 nodes are smaller and dimmer. Labels are
// truncated by CSS (max-width + ellipsis, tighter per depth) but the full title always
// stays reachable: it's the button's accessible name (aria-label) and its native title
// attribute (hover tooltip) — never just the visually-clipped text.
//
// The visible label can't use RichTitle's <code> here: it's plain text content laid over the
// ego-graph's <svg> canvas, sized/truncated by CSS tuned for a single text run, so backticks
// are stripped (not rendered as <code>) for this one display surface only. aria-label and the
// native title= tooltip below keep the raw, un-stripped title.

export function GraphNodes({
  nodes,
  positions,
  width,
  height,
  hovered,
  neighbours,
  onHover,
  onOpen,
}: {
  nodes: EgoNode[];
  positions: Map<string, Point>;
  width: number;
  height: number;
  hovered: string | null;
  neighbours: Set<string>;
  onHover: (uid: string | null) => void;
  onOpen: (ref: PageRef) => void;
}) {
  return (
    <>
      {nodes.map((node) => {
        const point = positions.get(node.uid);
        if (!point) return null;
        const dim = hovered !== null && hovered !== node.uid && !neighbours.has(node.uid);
        const label = node.depth === 0 ? `${node.title} (centre)` : node.title;
        return (
          <button
            key={node.uid}
            type="button"
            className="graph-node"
            data-type={node.type}
            data-depth={node.depth}
            data-dim={dim ? "true" : "false"}
            aria-label={label}
            title={node.title}
            style={{ left: `${(point.x / width) * 100}%`, top: `${(point.y / height) * 100}%` }}
            onMouseEnter={() => onHover(node.uid)}
            onMouseLeave={() => onHover(null)}
            onFocus={() => onHover(node.uid)}
            onBlur={() => onHover(null)}
            onClick={() => onOpen({ scope: node.scope, slug: node.slug })}
          >
            <span className="graph-node-dot" data-type={node.type} aria-hidden="true" />
            <span className="graph-node-label">{stripBackticks(node.title)}</span>
          </button>
        );
      })}
    </>
  );
}
