// Depth control for the ego-graph (spec §6.7 F-61): 1 or 2 hops from the centre,
// defaulting to 1. Showing the direct-neighbour count here means the user knows what a
// depth-1 view is hiding even before they switch to depth 2.

const DEPTHS = [1, 2] as const;

export function GraphControls({
  depth,
  onDepthChange,
  directNeighbourCount,
}: {
  depth: 1 | 2;
  onDepthChange: (depth: 1 | 2) => void;
  directNeighbourCount: number;
}) {
  return (
    <div className="graph-controls">
      <div className="graph-depth" role="group" aria-label="Neighbourhood depth">
        {DEPTHS.map((d) => (
          <button
            key={d}
            type="button"
            aria-pressed={depth === d}
            onClick={() => onDepthChange(d)}
          >
            Depth {d}
          </button>
        ))}
      </div>
      <span className="graph-neighbour-count">
        {directNeighbourCount} direct {directNeighbourCount === 1 ? "neighbour" : "neighbours"}
      </span>
    </div>
  );
}
