// Legend for the ego-graph (spec §6.7 F-61): lists only the edge roles actually present
// in the rendered (capped) subgraph, plus the shown/omitted node counts so it's plain
// when the cap has elided part of the neighbourhood ("N more not shown" beats a hairball).

export function GraphLegend({
  roles,
  nodeCount,
  edgeCount,
  omittedCount,
}: {
  roles: string[];
  nodeCount: number;
  edgeCount: number;
  omittedCount: number;
}) {
  return (
    <div className="graph-legend">
      {roles.map((role) => (
        <span key={role} className="graph-legend-item">
          <span className="edge-dot" data-role={role} aria-hidden="true" />
          {role}
        </span>
      ))}
      <span className="graph-hint">
        {nodeCount} shown · {edgeCount} relations
        {omittedCount > 0 ? ` · ${omittedCount} more not shown` : ""}
      </span>
    </div>
  );
}
