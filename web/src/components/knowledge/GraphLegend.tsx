import type { TypeName } from "../../lib/types";

// Legend for both graph modes: a dot per page type and a line per relation role, listing only
// what is actually on the canvas (the capped ego subgraph, or the filtered global graph). The
// swatch carries the hue; the label stays ink, since several type hues fail contrast as text.

export function GraphLegend({
  roles,
  types = [],
}: {
  roles: string[];
  types?: TypeName[];
}) {
  if (roles.length === 0 && types.length === 0) return null;
  return (
    <ul className="graph-legend" aria-label="Legend">
      {types.map((type) => (
        <li key={`type:${type}`} className="graph-legend-item">
          <span className="type-dot" data-type={type} aria-hidden="true" />
          {type}
        </li>
      ))}
      {roles.map((role) => (
        <li key={`role:${role}`} className="graph-legend-item">
          <span className="edge-dot" data-role={role} aria-hidden="true" />
          {role}
        </li>
      ))}
    </ul>
  );
}
