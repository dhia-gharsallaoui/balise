import type { Finding, SpaceNode, TypeName } from "../../lib/types";

export interface SpaceTreeProps {
  tree: SpaceNode[];
  types: { name: TypeName; count: number }[];
  historicalCount: number;
  selectedSpace: string | null;
  hiddenTypes: Set<string>;
  showHistorical: boolean;
  onSelectSpace: (name: string | null) => void;
  onToggleType: (type: string) => void;
  onToggleHistorical: () => void;
  // Findings that name no document. An unparseable page has no uid, so it has no row in any
  // list and no page to open — without a home here it is simply absent from the screen with
  // nothing saying why. Optional, and rendered only when non-empty.
  globalLint?: Finding[];
}

export function SpaceTree({
  tree,
  types,
  historicalCount,
  selectedSpace,
  hiddenTypes,
  showHistorical,
  onSelectSpace,
  onToggleType,
  onToggleHistorical,
  globalLint,
}: SpaceTreeProps) {
  return (
    <aside className="kn-left">
      <div className="kn-spaces">
        <div className="kn-section-title">Spaces</div>
        <button
          type="button"
          className="kn-space-row kn-space-all"
          aria-current={selectedSpace === null ? "true" : "false"}
          onClick={() => onSelectSpace(null)}
        >
          All spaces
        </button>
        {tree.map((node) => (
          <SpaceBranch
            key={`${node.scope}:${node.name}`}
            node={node}
            depth={0}
            selectedSpace={selectedSpace}
            onSelectSpace={onSelectSpace}
          />
        ))}
      </div>

      <div className="kn-filters">
        <div className="kn-section-title">Types</div>
        {types.map((t) => (
          <label key={t.name} className="kn-filter-row">
            <input
              type="checkbox"
              checked={!hiddenTypes.has(t.name)}
              onChange={() => onToggleType(t.name)}
              aria-label={t.name}
            />
            <span className="kn-filter-name">{t.name}</span>
            <span className="kn-filter-count">{t.count}</span>
          </label>
        ))}

        <label className="kn-filter-row kn-filter-historical">
          <input
            type="checkbox"
            checked={showHistorical}
            onChange={onToggleHistorical}
            aria-label="Show historical"
          />
          <span className="kn-filter-name">Historical</span>
          <span className="kn-filter-count">{historicalCount}</span>
        </label>
      </div>

      {globalLint && globalLint.length > 0 ? (
        <div className="kn-global-lint" role="status">
          <div className="kn-section-title">Not indexed</div>
          {globalLint.map((finding) => (
            <p key={`${finding.rule}:${finding.detail}`} className="kn-global-lint-row">
              <span className="kn-global-lint-rule">{finding.rule}</span>
              <span className="kn-global-lint-detail">{finding.detail}</span>
            </p>
          ))}
        </div>
      ) : null}
    </aside>
  );
}

function SpaceBranch({
  node,
  depth,
  selectedSpace,
  onSelectSpace,
}: {
  node: SpaceNode;
  depth: number;
  selectedSpace: string | null;
  onSelectSpace: (name: string | null) => void;
}) {
  const isSelected = selectedSpace === node.name;
  return (
    <>
      <button
        type="button"
        className="kn-space-row"
        style={{ paddingLeft: 8 + depth * 14 }}
        aria-current={isSelected ? "true" : "false"}
        onClick={() => onSelectSpace(isSelected ? null : node.name)}
      >
        <span className="kn-space-name">{node.name}</span>
        <span className="kn-space-count">{node.count}</span>
      </button>
      {node.children.map((child) => (
        <SpaceBranch
          key={`${child.scope}:${child.name}`}
          node={child}
          depth={depth + 1}
          selectedSpace={selectedSpace}
          onSelectSpace={onSelectSpace}
        />
      ))}
    </>
  );
}
