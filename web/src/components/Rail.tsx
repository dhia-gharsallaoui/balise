import {
  BookOpenText,
  House,
  Monitor,
  Moon,
  Plugs,
  SlidersHorizontal,
  Stamp,
  Sun,
  Tray,
  type Icon,
} from "@phosphor-icons/react";
import type { Preference } from "../lib/theme";
import { plural } from "../lib/plural";

export const SECTIONS = ["Home", "Knowledge", "Review", "Sources", "Agents", "Settings"] as const;
export type Section = (typeof SECTIONS)[number];

const ICONS: Record<Section, Icon> = {
  Home: House,
  Knowledge: BookOpenText,
  Review: Stamp,
  Sources: Tray,
  Agents: Plugs,
  Settings: SlidersHorizontal,
};

export const THEME_OPTIONS: { value: Preference; label: string; icon: Icon }[] = [
  { value: "system", label: "System", icon: Monitor },
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
];

interface Props {
  active: Section;
  onSelect: (section: Section) => void;
  scopes: { name: string; count: number }[];
  preference: Preference;
  onSetTheme: (preference: Preference) => void;
  // Pending proposals, shown beside Review so the queue is visible from every screen.
  reviewCount?: number;
}

export function sectionIcon(section: Section): Icon {
  return ICONS[section];
}

export function Rail({ active, onSelect, scopes, preference, onSetTheme, reviewCount = 0 }: Props) {
  const total = scopes.reduce((sum, s) => sum + s.count, 0);
  return (
    <nav className="rail" aria-label="Sections">
      <div className="rail-brand">
        <span className="rail-mark" aria-hidden="true" />
        <div className="rail-brand-text">
          <div className="rail-name">Balise</div>
          <div className="rail-sub">
            {plural(scopes.length, "scope")}, {plural(total, "page")}
          </div>
        </div>
      </div>

      <div className="rail-nav">
        {SECTIONS.map((section) => {
          const SectionIcon = ICONS[section];
          const current = section === active;
          return (
            <button
              key={section}
              type="button"
              className="rail-item"
              aria-current={current ? "page" : undefined}
              onClick={() => onSelect(section)}
            >
              <SectionIcon className="rail-icon" size={18} weight={current ? "fill" : "regular"} aria-hidden="true" />
              <span className="rail-item-label">{section}</span>
              {section === "Review" && reviewCount > 0 ? (
                <span className="count rail-badge">
                  {reviewCount}
                  <span className="sr-only"> waiting</span>
                </span>
              ) : null}
            </button>
          );
        })}
      </div>

      {scopes.length > 0 ? (
        <div className="rail-scopes">
          <div className="rail-scopes-title" id="rail-scopes-title">Scopes</div>
          <ul aria-labelledby="rail-scopes-title">
            {scopes.map((scope) => (
              <li key={scope.name} className="rail-scope">
                <span className="rail-scope-name">{scope.name}</span>
                <span className="rail-count">{scope.count}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="rail-foot">
        <div className="segmented rail-theme" role="group" aria-label="Theme">
          {THEME_OPTIONS.map(({ value, label, icon: ThemeIcon }) => (
            <button
              key={value}
              type="button"
              aria-pressed={preference === value}
              title={`${label} theme`}
              onClick={() => onSetTheme(value)}
            >
              <ThemeIcon size={14} aria-hidden="true" />
              <span>{label}</span>
            </button>
          ))}
        </div>
      </div>
    </nav>
  );
}
