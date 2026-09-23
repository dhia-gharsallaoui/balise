import type { Group, PageRef, PageRow as Row } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { PageRow } from "./PageRow";

// Group order is fixed by the API (state, decision, gotcha, procedure, issue,
// incident, note, memory) — this rank table is a defensive no-op for browse
// data, which already arrives in this order. It must never re-sort
// alphabetically.
const ORDER = ["state", "decision", "gotcha", "procedure", "issue", "incident", "note", "memory"];

const rank = (type: string) => {
  const index = ORDER.indexOf(type);
  return index === -1 ? ORDER.length : index;
};

interface Props {
  // Browse mode's rows, still type-grouped and rendered with section headers.
  groups: Group[];
  // Search mode's rows, already in the exact relevance order /api/search returned. Rendered
  // as one flat list with no section headers, so nothing between the API and the screen ever
  // reorders a ranked result — see search-ranking-coverage-report.md (FIX 1). Type is not
  // lost by dropping the group header: each row prints its own TypeChip instead (PageRow's
  // showType prop).
  rows: Row[];
  query: string;
  // null means "not in search mode" (browse mode has no coverage signal);
  // treated the same as "ok" — no coverage banner.
  coverage: "ok" | "low" | null;
  onOpen: (ref: PageRef) => void;
}

export function ResultList({ groups, rows, query, coverage, onOpen }: Props) {
  // Mirrors Knowledge.tsx's own query.trim() check (its data-fetch effect uses the same
  // boundary to decide whether to call search() or fetchPages()) so this component never
  // disagrees with its caller about which of groups/rows is the live one to render.
  const searching = query.trim().length > 0;

  if (searching) {
    if (rows.length === 0) {
      return (
        <EmptyState
          title={`Nothing matches “${query}”.`}
          hint="Try fewer words, or turn on historical pages."
        />
      );
    }
    return (
      <div className="result-list">
        {coverage === "low" ? (
          <p className="coverage-note" role="status">
            This knowledge base holds little about this. Treat what follows as thin.
          </p>
        ) : null}
        <ul className="result-rows">
          {rows.map((page) => (
            <PageRow key={`${page.scope}:${page.slug}`} page={page} onOpen={onOpen} showType />
          ))}
        </ul>
      </div>
    );
  }

  const ordered = [...groups].sort((a, b) => rank(a.type) - rank(b.type));

  if (ordered.length === 0) {
    return <EmptyState title="Nothing in this space yet." hint="Add a page or drop a file." />;
  }

  return (
    <div className="result-list">
      {ordered.map((group) => (
        <section key={group.type} className="result-group">
          <h3 className="result-group-head">
            <span className="type-dot" data-type={group.type} aria-hidden="true" />
            <span>{group.type}</span>
            <span className="kn-count">{group.count}</span>
          </h3>
          <ul className="result-rows">
            {group.pages.map((page) => (
              <PageRow key={`${page.scope}:${page.slug}`} page={page} onOpen={onOpen} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
