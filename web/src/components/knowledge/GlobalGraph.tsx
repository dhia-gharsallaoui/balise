import { useMemo, useState } from "react";
import type { GraphEdge, PageRef } from "../../lib/types";
import { GlobalGraphNodes } from "./GlobalGraphNodes";
import type { GlobalGraph as GlobalGraphData } from "./globalGraph";
import { layoutGlobalGraph, panelSizeClass } from "./globalLayout";
import { GraphEdges } from "./GraphEdges";
import { GraphLegend } from "./GraphLegend";

// The whole-vault view (GraphView's no-centre branch): five scopes render as five
// independent panels — "islands" — because scope is the security boundary in this corpus
// (zero edges ever cross it) and the layout should make that a visual fact, not just a
// data fact. Each panel is its own independently laid-out canvas (a radial spanning tree
// for the connected nodes — treeLayout.ts, via globalLayout.ts — plus a separate isolated
// shelf); an edge is never drawn between two panels because no edge in this graph ever
// needs to be.

const SEED = 42;
const EMPTY_SET = new Set<string>();

export function GlobalGraph({
  graph,
  onOpen,
}: {
  graph: GlobalGraphData;
  onOpen: (ref: PageRef) => void;
}) {
  const [hovered, setHovered] = useState<string | null>(null);

  // Keyed on `graph` (a single object GraphView only rebuilds when the fetched payload,
  // the centre/no-centre toggle, or the caller's allowedUids filter changes) — hovering a
  // node updates local state here, which does not touch `graph`, so this never re-runs the
  // force simulation on hover.
  const layouts = useMemo(() => layoutGlobalGraph(graph.panels, SEED), [graph]);
  const roles = useMemo(
    () => [...new Set(graph.panels.flatMap((p) => p.edges.map((e) => e.kind)))],
    [graph],
  );

  return (
    <div className="graph-frame global-graph">
      <GraphLegend roles={roles} nodeCount={graph.totalNodes} edgeCount={graph.totalEdges} omittedCount={0} />
      <p className="global-summary">
        {graph.scopeCount} {graph.scopeCount === 1 ? "scope" : "scopes"}
        {graph.isolatedCount > 0
          ? ` · ${graph.isolatedCount} isolated — no recorded relations, shelved below each scope`
          : ""}
      </p>
      <div className="global-panels">
        {graph.panels.map((panel, i) => {
          const layout = layouts[i];
          const total = panel.connected.length + panel.isolated.length;
          const neighbours = neighboursOf(panel.edges, hovered);
          return (
            <section
              className="global-panel"
              key={panel.scope}
              aria-label={`${panel.scope} scope`}
              data-size={panelSizeClass(total)}
            >
              <header className="global-panel-header">
                <h3>{panel.scope}</h3>
                <span>{total} pages</span>
              </header>

              {panel.connected.length > 0 ? (
                <div
                  className="global-panel-canvas"
                  style={{ aspectRatio: `${layout.width} / ${layout.connectedHeight}` }}
                >
                  <GraphEdges
                    edges={panel.edges}
                    positions={layout.connectedPositions}
                    width={layout.width}
                    height={layout.connectedHeight}
                    nodeCount={panel.connected.length}
                    hovered={hovered}
                    scopeLabel={panel.scope}
                    primaryEdgeIndices={layout.treeEdgeIndices}
                  />
                  <GlobalGraphNodes
                    nodes={panel.connected}
                    positions={layout.connectedPositions}
                    width={layout.width}
                    height={layout.connectedHeight}
                    hubUids={layout.hubUids}
                    hovered={hovered}
                    neighbours={neighbours}
                    onHover={setHovered}
                    onOpen={onOpen}
                  />
                </div>
              ) : null}

              {panel.isolated.length > 0 ? (
                <div className="global-panel-shelf">
                  <p className="global-shelf-label">
                    {panel.isolated.length} isolated — no recorded relations
                  </p>
                  <div
                    className="global-panel-canvas global-panel-canvas-shelf"
                    style={{ aspectRatio: `${layout.width} / ${layout.isolatedHeight}` }}
                  >
                    <GlobalGraphNodes
                      nodes={panel.isolated}
                      positions={layout.isolatedPositions}
                      width={layout.width}
                      height={layout.isolatedHeight}
                      hubUids={EMPTY_SET}
                      hovered={hovered}
                      neighbours={EMPTY_SET}
                      onHover={setHovered}
                      onOpen={onOpen}
                      isolated
                    />
                  </div>
                </div>
              ) : null}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function neighboursOf(edges: GraphEdge[], hovered: string | null): Set<string> {
  if (!hovered) return EMPTY_SET;
  return new Set(
    edges
      .filter((e) => e.from_uid === hovered || e.to_uid === hovered)
      .flatMap((e) => [e.from_uid, e.to_uid]),
  );
}
