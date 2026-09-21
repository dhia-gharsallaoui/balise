import { useEffect, useState } from "react";
import { fetchHome, fetchTree } from "../lib/api";
import { applyTheme, readPreference, type Preference } from "../lib/theme";
import { current as currentUrl, DEFAULT_PARAMS, onPopState, write as writeUrl } from "../lib/urlState";
import type { HomePageRef, HomeResponse, TreeResponse } from "../lib/types";
import { AgentsScreen } from "./agents/AgentsScreen";
import { Home } from "./home/Home";
import { Knowledge } from "./knowledge/Knowledge";
import { Rail, SECTIONS, type Section } from "./Rail";
import { ReviewScreen } from "./review/ReviewScreen";
import { SettingsScreen } from "./settings/SettingsScreen";
import { SourcesScreen } from "./sources/SourcesScreen";
import { EmptyState } from "./ui/EmptyState";
import "../styles/shell.css";

// The pages a "Needs attention" sentence points at, carried over to Knowledge as a filter
// so clicking it lands on exactly the pages the sentence was about, plus the sentence itself
// so Knowledge can show what the viewer is looking at and how to clear it.
interface AttentionFilter {
  pages: HomePageRef[];
  label: string;
}

const CYCLE: Preference[] = ["system", "light", "dark"];

const SUBTITLES: Record<Section, string> = {
  Home: "What needs you today",
  Knowledge: "",
  Review: "Proposed changes, newest first",
  Sources: "Where knowledge comes in",
  Agents: "Who may read what",
  Settings: "Structure, scopes, storage, models, export",
};

// Maps the URL's section segment onto a real section. An unknown or absent segment falls
// back to Knowledge — the app's long-standing landing screen — rather than erroring, so a
// mistyped or stale link still opens something useful.
function sectionFromPath(segment: string | null): Section {
  if (!segment) return "Knowledge";
  return SECTIONS.find((s) => s.toLowerCase() === segment) ?? "Knowledge";
}

export function AppShell() {
  const [section, setSectionState] = useState<Section>(() => sectionFromPath(currentUrl().section));
  const [tree, setTree] = useState<TreeResponse | null>(null);
  const [home, setHome] = useState<HomeResponse | null>(null);
  const [homeError, setHomeError] = useState<string | null>(null);
  const [attentionFilter, setAttentionFilter] = useState<AttentionFilter | null>(null);
  const [preference, setPreference] = useState<Preference>(readPreference);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { applyTheme(preference); }, [preference]);

  // Changing section is a discrete navigation, so it pushes a history entry — back returns
  // you to the screen you came from. It also resets every Knowledge-owned param: those
  // describe a view of the knowledge base, and carrying `?q=alloy` onto /review would
  // describe nothing. Going back still restores them, because the previous URL is intact
  // in history.
  function setSection(next: Section) {
    setSectionState(next);
    writeUrl({ ...DEFAULT_PARAMS, section: next.toLowerCase() });
  }

  // Back/forward rewrites the address bar without telling React; this is what keeps the
  // rendered section in step with it. Knowledge subscribes separately for its own params.
  useEffect(() => onPopState((p) => setSectionState(sectionFromPath(p.section))), []);

  useEffect(() => {
    fetchTree().then(setTree).catch((e) => setError(e.message));
  }, []);

  // Home and Review share one fetch of /api/home: the queue Home summarizes by kind is the
  // same list Review lists in full, and there is no reason to ask the server for it twice.
  useEffect(() => {
    fetchHome().then(setHome).catch((e) => setHomeError(e.message));
  }, []);

  function openReview() {
    setSection("Review");
  }

  function filterAttention(pages: HomePageRef[], label: string) {
    setAttentionFilter({ pages, label });
    setSection("Knowledge");
  }

  // Review's evidence always cites the same page the proposal targets (confirmed against
  // real proposal data: evidence[].source matches scope/target), so "open the raw page"
  // needs only that one (scope, slug) pair, not a general navigation-by-arbitrary-path
  // mechanism. Reuses filterAttention's Knowledge-restrict trick with a single-page list.
  function openPageFromReview(scope: string, slug: string, title: string) {
    filterAttention([{ scope, slug, title }], `From review: ${title}`);
  }

  const scopes = collectScopes(tree);
  const subtitle = section === "Knowledge"
    ? `${tree?.spaces.reduce((n, s) => n + s.count, 0) ?? 0} pages across ${scopes.length} scopes`
    : SUBTITLES[section];

  return (
    <div className="app">
      <Rail
        active={section}
        onSelect={setSection}
        scopes={scopes}
        preference={preference}
        onCycleTheme={() => setPreference(CYCLE[(CYCLE.indexOf(preference) + 1) % CYCLE.length])}
      />
      <main className="main">
        <header className="topbar">
          <div className="topbar-heading">
            <div className="topbar-title">{section}</div>
            <div className="topbar-sub">{subtitle}</div>
          </div>
        </header>
        {error ? (
          <EmptyState title="Could not reach the server." hint={`${error}. Is balise serve running?`} />
        ) : section === "Knowledge" ? (
          tree ? (
            <Knowledge
              tree={tree}
              restrictTo={attentionFilter}
              onClearRestrict={() => setAttentionFilter(null)}
            />
          ) : null
        ) : section === "Home" ? (
          <Home
            home={home}
            error={homeError}
            onOpenReview={openReview}
            onFilterAttention={filterAttention}
          />
        ) : section === "Review" ? (
          <ReviewScreen onOpenPage={openPageFromReview} />
        ) : section === "Agents" ? (
          <AgentsScreen allScopes={scopes.map((s) => s.name)} />
        ) : section === "Sources" ? (
          <SourcesScreen allScopes={scopes.map((s) => s.name)} onOpenPage={filterAttention} />
        ) : section === "Settings" ? (
          <SettingsScreen />
        ) : (
          <EmptyState
            title={`${section} is not part of this release.`}
            hint="The Knowledge section is what this slice builds."
          />
        )}
      </main>
    </div>
  );
}

function collectScopes(tree: TreeResponse | null) {
  if (!tree) return [];
  const counts = new Map<string, number>();
  for (const space of tree.spaces) {
    counts.set(space.scope, (counts.get(space.scope) ?? 0) + space.count);
  }
  return [...counts].map(([name, count]) => ({ name, count }));
}

export { SECTIONS };
