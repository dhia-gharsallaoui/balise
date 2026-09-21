import { useEffect, useMemo, useState } from "react";
import { fetchGraph } from "../../lib/api";
import type { GraphResponse, PageRef } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { buildEgoGraph, type EgoGraph } from "./ego";
import { GlobalGraph } from "./GlobalGraph";
import { buildGlobalGraph } from "./globalGraph";
import { GraphControls } from "./GraphControls";
import { GraphEdges } from "./GraphEdges";
import { GraphLegend } from "./GraphLegend";
import { GraphNodes } from "./GraphNodes";
import { layoutGraph } from "./layout";
import "../../styles/graph.css";

// Ego-graph view (spec §6.7 F-61): centred on whatever page is open in the reader, not
// the whole corpus — 02-ui-design-v1.md §9 cut graph view for exactly the hairball
// problem that rendering all 138 nodes/190 edges at once reproduced. See ego.ts for why
// the bounded neighbourhood is computed client-side from the existing /api/graph payload
// rather than a new server endpoint.
//
// The layout stays a pure function of (nodes, edges, seed) — same SEED, and ego.ts
// returns nodes/edges in a deterministic order — so the same centre page always draws
// the same picture.
//
// When no page is open, this renders the whole-vault "global graph" instead (GlobalGraph
// —see globalGraph.ts for why five scopes read as five islands). Both modes hang off this
// same component and the same fetched `graph`; `centre` is the only switch between them,
// and the ego branch below is unchanged from before the global view existed.
const WIDTH = 860;
const HEIGHT = 560;
const SEED = 42;

export function GraphView({
  centre,
  onOpen,
  allowedUids,
}: {
  centre: PageRef | null;
  onOpen: (ref: PageRef) => void;
  // Knowledge.tsx's on-screen space/type filters, expressed as the set of uids they leave
  // visible — derived there from the same visibleGroups the List view renders, so both
  // views agree on what's shown. Only consulted by the global (no-centre) branch: the ego
  // view's neighbourhood is defined by which page is open, not by these filters.
  allowedUids?: Set<string> | null;
}) {
  const [graph, setGraph] = useState<GraphResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [depth, setDepth] = useState<1 | 2>(1);

  useEffect(() => {
    let cancelled = false;
    fetchGraph()
      .then((res) => !cancelled && setGraph(res))
      .catch((e: unknown) => !cancelled && setError(messageOf(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  const ego: EgoGraph | null = useMemo(
    () => (graph && centre ? buildEgoGraph(graph, centre, depth) : null),
    [graph, centre, depth],
  );

  const positions = useMemo(
    () =>
      ego
        ? layoutGraph(ego.nodes, ego.edges, { width: WIDTH, height: HEIGHT, seed: SEED })
        : new Map(),
    [ego],
  );

  const global = useMemo(
    () => (graph && !centre ? buildGlobalGraph(graph, allowedUids ?? null) : null),
    [graph, centre, allowedUids],
  );

  if (error) return <p className="kn-error" role="alert">{error}</p>;

  if (!graph) return <div className="graph-frame" aria-busy="true" />;

  if (!centre) {
    if (!global || global.totalNodes === 0) {
      return (
        <EmptyState
          title="Nothing to show."
          hint="No pages match the current space and type filters."
        />
      );
    }
    return <GlobalGraph graph={global} onOpen={onOpen} />;
  }

  if (!ego || ego.nodes.length <= 1) {
    return (
      <EmptyState
        title="No relations to draw yet."
        hint="This page has no typed refs or wikilinks connecting it to another page."
      />
    );
  }

  const roles = [...new Set(ego.edges.map((e) => e.kind))];
  const neighbours = neighboursOf(ego, hovered);

  return (
    <div className="graph-frame">
      <GraphControls depth={depth} onDepthChange={setDepth} directNeighbourCount={ego.directNeighbourCount} />
      <GraphLegend
        roles={roles}
        nodeCount={ego.nodes.length}
        edgeCount={ego.edges.length}
        omittedCount={ego.omittedCount}
      />
      <div className="graph-canvas">
        <GraphEdges edges={ego.edges} positions={positions} width={WIDTH} height={HEIGHT} nodeCount={ego.nodes.length} hovered={hovered} />
        <GraphNodes
          nodes={ego.nodes}
          positions={positions}
          width={WIDTH}
          height={HEIGHT}
          hovered={hovered}
          neighbours={neighbours}
          onHover={setHovered}
          onOpen={onOpen}
        />
      </div>
    </div>
  );
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown error";
}

function neighboursOf(ego: EgoGraph, hovered: string | null): Set<string> {
  if (!hovered) return new Set();
  return new Set(
    ego.edges
      .filter((e) => e.from_uid === hovered || e.to_uid === hovered)
      .flatMap((e) => [e.from_uid, e.to_uid]),
  );
}
