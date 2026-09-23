import { useState } from "react";
import type { PageRef, PageRow as Row } from "../../lib/types";
import { RichTitle } from "../ui/RichTitle";
import { StatusWord } from "../ui/StatusWord";
import { TypeChip } from "../ui/TypeChip";

// The row that appears everywhere (spec `01` §0: "design the row once"). The
// twist expands claims in place; the row itself opens the page in the reader.
// These must stay two separate click targets — see pagerow.test.tsx's
// "does not open the page when only the twist is activated".
//
// showType (default false, unchanged for every existing caller) prints the page's type as
// a TypeChip in the subtitle line. It exists for ResultList's search-result mode: FIX 1
// (search-ranking-coverage-report.md) stops bucketing search hits into per-type sections so
// the API's relevance order survives to the screen, which means the type can no longer be
// read off a group header — this is where that signal moves to instead. Browse mode keeps
// its type-grouped headers untouched and never passes this prop, so its rows render exactly
// as before.
export function PageRow({
  page,
  onOpen,
  showType = false,
}: {
  page: Row;
  onOpen: (ref: PageRef) => void;
  showType?: boolean;
}) {
  const [open, setOpen] = useState(false);
  // When the title comes from the hook rung, claims[0] duplicates the title text exactly —
  // correct per the design, but it must not be printed twice in the expanded claim list. See
  // the matching suppression in PageDetail.tsx.
  const visibleClaims = page.claims.filter((claim) => claim.text !== page.title);
  const hasClaims = visibleClaims.length > 0;

  return (
    <li className="page-row" data-historical={page.historical ? "true" : "false"}>
      <div className="page-row-head">
        <button
          type="button"
          className="page-twist"
          aria-expanded={open}
          aria-label={`${open ? "Collapse" : "Expand"} claims for ${page.title}`}
          disabled={!hasClaims}
          onClick={() => setOpen((v) => !v)}
        >
          {hasClaims ? (open ? "▾" : "▸") : ""}
        </button>

        <button
          type="button"
          className="page-open"
          onClick={() => onOpen({ scope: page.scope, slug: page.slug })}
        >
          <span className="page-title"><RichTitle title={page.title} /></span>
          <span className="page-sub">
            {showType ? (
              <>
                <TypeChip type={page.type} />
                {" · "}
              </>
            ) : null}
            {page.slug}
            {page.claims_count > 0
              ? ` · ${page.claims_count} claim${page.claims_count === 1 ? "" : "s"}`
              : ""}
          </span>
        </button>

        <StatusWord status={page.status} />
      </div>

      {open && hasClaims ? (
        <ul className="claim-list">
          {visibleClaims.map((claim, index) => (
            <li
              key={index}
              className="claim-row"
              data-historical={claim.status !== "active" ? "true" : "false"}
            >
              <span className="claim-text">{claim.text}</span>
              <StatusWord status={claim.status} asOf={claim.as_of} />
            </li>
          ))}
        </ul>
      ) : null}
    </li>
  );
}
