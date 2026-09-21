import type { Preference } from "../lib/theme";

export const SECTIONS = ["Home", "Knowledge", "Review", "Sources", "Agents", "Settings"] as const;
export type Section = (typeof SECTIONS)[number];

interface Props {
  active: Section;
  onSelect: (section: Section) => void;
  scopes: { name: string; count: number }[];
  preference: Preference;
  onCycleTheme: () => void;
}

export function Rail({ active, onSelect, scopes, preference, onCycleTheme }: Props) {
  const total = scopes.reduce((sum, s) => sum + s.count, 0);
  return (
    <nav className="rail" aria-label="Sections">
      <div className="rail-brand">
        <span className="rail-mark" aria-hidden="true" />
        <div className="rail-brand-text">
          <div className="rail-name">Knowledge</div>
          <div className="rail-sub">{scopes.length} scopes · {total} pages</div>
        </div>
      </div>

      <button className="rail-palette" disabled title="Available in a later release">
        <span>Search or jump to…</span>
        <span className="kbd">⌘K</span>
      </button>

      <div className="rail-nav">
        {SECTIONS.map((section) => (
          <button
            key={section}
            className="rail-item"
            aria-current={section === active ? "page" : undefined}
            onClick={() => onSelect(section)}
          >
            <span className="rail-dot" aria-hidden="true" />
            <span className="rail-item-label">{section}</span>
          </button>
        ))}
      </div>

      <div className="rail-scopes">
        <div className="rail-scopes-title">Scopes</div>
        {scopes.map((scope) => (
          <div key={scope.name} className="rail-scope">
            <span className="rail-dot" aria-hidden="true" />
            <span>{scope.name}</span>
            <span className="rail-count">{scope.count}</span>
          </div>
        ))}
      </div>

      <div className="rail-foot">
        <button onClick={onCycleTheme} aria-label={`Theme: ${preference}. Change theme.`}>
          <span className="theme-toggle-label">Theme · {preference}</span>
        </button>
      </div>
    </nav>
  );
}
