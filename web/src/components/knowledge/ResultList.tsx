import type { Group, PageRef } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { PageRow } from "./PageRow";

// Group order is fixed by the API (state, decision, gotcha, procedure, issue,
// incident, note, memory) — this rank table is a defensive no-op for browse
// data (already arrives in this order) and the actual fix for search data
// (grouped through a Map in Knowledge.tsx, whose insertion order tracks first
// occurrence in the hit list, not the canonical type order). It must never
// re-sort alphabetically.
const ORDER = ["state", "decision", "gotcha", "procedure", "issue", "incident", "note", "memory"];

const rank = (type: string) => {
  const index = ORDER.indexOf(type);
  return index === -1 ? ORDER.length : index;
};

interface Props {
  groups: Group[];
  query: string;
  // null means "not in search mode" (browse mode has no coverage signal);
  // treated the same as "ok" — no coverage banner.
  coverage: "ok" | "low" | null;
  onOpen: (ref: PageRef) => void;
}

export function ResultList({ groups, query, coverage, onOpen }: Props) {
  const ordered = [...groups].sort((a, b) => rank(a.type) - rank(b.type));

  if (ordered.length === 0) {
    return query ? (
      <EmptyState
        title={`Nothing matches “${query}”.`}
        hint="Try fewer words, or turn on historical pages."
      />
    ) : (
      <EmptyState title="Nothing in this space yet." hint="Add a page or drop a file." />
    );
  }

  return (
    <div className="result-list">
      {coverage === "low" ? (
        <p className="coverage-note" role="status">
          This knowledge base holds little about this. Treat what follows as thin.
        </p>
      ) : null}

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
