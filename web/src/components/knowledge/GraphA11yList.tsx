import { stripBackticks } from "../../lib/richTitle";
import type { PageRef } from "../../lib/types";
import type { EgoGraph } from "./ego";
import type { GlobalGraph } from "./globalGraph";

// Cytoscape draws to a <canvas>, which is invisible to screen readers and unreachable by
// keyboard — a real regression from the previous DOM-per-node implementation, whose whole
// reason for existing (real focusable elements, one per node) is what made a11y.spec.ts's
// axe scans and keyboard-open test pass. This component is that equivalent: a visually
// hidden (see .sr-only in graph.css) but fully focusable and keyboard-operable list of the
// same nodes and relations, rendered as a sibling of the Cytoscape canvas rather than a
// replacement for it. `.cy-container`'s own `role="img"` collapses the canvas subtree into
// one atomic accessible object (a screen reader user gets this list instead, not both).
//
// Clicking a canvas node and activating this list's button both call the same `onOpen`, so
// neither path is a second-class citizen — the list simply exists for anyone who can't use
// the canvas at all.

interface GraphA11yListProps {
  mode: "ego" | "global";
  ego?: EgoGraph;
  global?: GlobalGraph;
  collapsedScopes?: ReadonlySet<string>;
  onOpen: (ref: PageRef) => void;
  onToggleScope?: (scope: string) => void;
}

export function GraphA11yList({
  mode,
  ego,
  global,
  collapsedScopes,
  onOpen,
  onToggleScope,
}: GraphA11yListProps) {
  if (mode === "ego" && ego) {
    return (
      <ul className="sr-only graph-a11y-list" aria-label={`Relation graph of ${ego.nodes.length} pages`}>
        {ego.nodes.map((node) => (
          <li key={node.uid}>
            <button
              type="button"
              className="graph-a11y-node"
              data-uid={node.uid}
              data-type={node.type}
              data-scope={node.scope}
              data-depth={node.depth}
              data-centre={node.depth === 0}
              aria-label={node.depth === 0 ? `${node.title} (centre)` : node.title}
              title={node.title}
              onClick={() => onOpen({ scope: node.scope, slug: node.slug })}
            >
              {stripBackticks(node.title)}
            </button>
          </li>
        ))}
      </ul>
    );
  }

  if (mode === "global" && global) {
    return (
      <div className="sr-only graph-a11y-list" aria-label={`Relation graph of ${global.totalNodes} pages`}>
        {global.scopes.map((group) => {
          const collapsed = collapsedScopes?.has(group.scope) ?? false;
          return (
            <section key={group.scope} className="graph-a11y-scope" aria-label={`${group.scope} scope`}>
              <button
                type="button"
                className="graph-a11y-scope-toggle"
                aria-expanded={!collapsed}
                onClick={() => onToggleScope?.(group.scope)}
              >
                {group.scope} ({group.nodes.length}){collapsed ? ", collapsed" : ""}
              </button>
              {!collapsed && (
                <ul>
                  {group.nodes.map((node) => (
                    <li key={node.uid}>
                      <button
                        type="button"
                        className="graph-a11y-node"
                        data-uid={node.uid}
                        data-type={node.type}
                        data-scope={node.scope}
                        data-degree={global.degree.get(node.uid) ?? 0}
                        data-isolated={(global.degree.get(node.uid) ?? 0) === 0}
                        aria-label={node.title}
                        title={node.title}
                        onClick={() => onOpen({ scope: node.scope, slug: node.slug })}
                      >
                        {stripBackticks(node.title)}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    );
  }

  return null;
}
