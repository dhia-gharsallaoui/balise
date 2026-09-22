import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchGraph } from "../../lib/api";
import type { GraphResponse, PageRef } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { buildEgoGraph, type EgoGraph } from "./ego";
import { GraphCanvas } from "./GraphCanvas";
import { buildEgoElements, buildGlobalElements } from "./graphElements";
import { buildGlobalGraph, type GlobalGraph } from "./globalGraph";
import { GraphControls } from "./GraphControls";
import { GraphLegend } from "./GraphLegend";
import "../../styles/graph.css";

// Ego-graph view (spec §6.7 F-61): centred on whatever page is open in the reader, not
// the whole corpus — 02-ui-design-v1.md §9 cut graph view for exactly the hairball
// problem that rendering all 138 nodes/190 edges at once reproduced. See ego.ts for why
// the bounded neighbourhood is computed client-side from the existing /api/graph payload
// rather than a new server endpoint.
//
// Both this ego branch and the whole-vault global branch (no page open) now render through
// the same Cytoscape-backed GraphCanvas: graphElements.ts turns whichever domain graph is
// active into Cytoscape elements, fcose lays them out, and GraphCanvas pairs the canvas
// with an always-present accessible list (GraphA11yList) so neither mode depends on a
// separate bespoke renderer the way the old force-simulated/radial-tree views did.

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
  const [depth, setDepth] = useState<1 | 2>(1);
  const [collapsedScopes, setCollapsedScopes] = useState<Set<string>>(new Set());

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

  const global: GlobalGraph | null = useMemo(
    () => (graph && !centre ? buildGlobalGraph(graph, allowedUids ?? null) : null),
    [graph, centre, allowedUids],
  );

  const egoElements = useMemo(() => (ego ? buildEgoElements(ego) : []), [ego]);
  const globalElements = useMemo(
    () => (global ? buildGlobalElements(global, collapsedScopes) : []),
    [global, collapsedScopes],
  );

  const toggleScope = useCallback((scope: string) => {
    setCollapsedScopes((prev) => {
      const next = new Set(prev);
      if (next.has(scope)) next.delete(scope);
      else next.add(scope);
      return next;
    });
  }, []);

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
    return (
      <div className="graph-frame">
        <p className="global-summary">
          {global.totalNodes} pages · {global.totalEdges} relations · {global.scopeCount}{" "}
          {global.scopeCount === 1 ? "scope" : "scopes"}
          {global.isolatedCount > 0 ? ` · ${global.isolatedCount} isolated` : ""}
        </p>
        <GraphCanvas
          mode="global"
          elements={globalElements}
          nodeCount={global.totalNodes}
          global={global}
          collapsedScopes={collapsedScopes}
          onOpen={onOpen}
          onToggleScope={toggleScope}
        />
      </div>
    );
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

  return (
    <div className="graph-frame">
      <GraphControls depth={depth} onDepthChange={setDepth} directNeighbourCount={ego.directNeighbourCount} />
      <GraphLegend
        roles={roles}
        nodeCount={ego.nodes.length}
        edgeCount={ego.edges.length}
        omittedCount={ego.omittedCount}
      />
      <GraphCanvas mode="ego" elements={egoElements} nodeCount={ego.nodes.length} ego={ego} onOpen={onOpen} />
    </div>
  );
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown error";
}
