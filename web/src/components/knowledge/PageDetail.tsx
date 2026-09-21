import { parseBody } from "../../lib/markdown";
import type { PageDetail } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { RichTitle } from "../ui/RichTitle";
import { StatusWord } from "../ui/StatusWord";
import { TypeChip } from "../ui/TypeChip";

const ROLE_LABELS: Record<string, string> = {
  specializes: "Instance of",
  about: "About",
  supersedes: "Supersedes",
  derives: "Derived from",
  owns: "Owns",
  link: "Mentioned by",
};

// Dedicated, full-width reading page (see reading-view-report.md): opening a page from the
// List view now renders this in place of the browse grid entirely, rather than sharing the
// screen with it in a narrow column — Knowledge.tsx only mounts this component when a page is
// open, so `page === null` here always means "still fetching it", never "nothing selected".
//
// Content and order are unchanged from the column this replaces (v3, task-6-brief): claims
// first, then the body, then where it came from. body_md is raw markdown — parseBody runs
// here in the browser; the server never reshapes a page body (`01` §1.2 principle 3). Two
// things are new: the back control is a real breadcrumb affordance instead of a bare "Close",
// and a Backlinks section (page.backlinks was already on the wire but rendered nowhere).
export function PageReader(
  { page, onBack }: { page: PageDetail | null; onBack: () => void },
) {
  if (!page) {
    return (
      <article className="kn-reader" aria-busy="true">
        <div className="kn-reader-inner">
          <div className="detail-crumb">
            <button type="button" className="detail-back" onClick={onBack}>
              Back to Knowledge
            </button>
          </div>
          <EmptyState
            title="Loading page…"
            hint="Fetching claims, body, and relations."
          />
        </div>
      </article>
    );
  }

  const blocks = parseBody(page.body_md);
  const warnings = page.lint.filter((l) => l.severity !== "suggest");
  // When the title comes from the hook rung (see resolveTitle in the importer), claims[0] is
  // the exact same text as the title — correct per the design (the headline IS the page's
  // most important claim), but it must not be printed twice. Suppress a claim whose text
  // exactly matches the title; if that empties the list, render nothing rather than an empty
  // "Claims" heading with no rows under it.
  const visibleClaims = page.claims.filter((claim) => claim.text !== page.title);

  return (
    <article className="kn-reader" aria-label={`Page: ${page.title}`}>
      <div className="kn-reader-inner">
        <div className="detail-crumb">
          <button type="button" className="detail-back" onClick={onBack}>
            Back to Knowledge
          </button>
        </div>

        <div className="detail-top">
          <TypeChip type={page.type} />
          {/* v3 binds this meta line to page.space, not page.scope — scope shows
              further down in the metadata table alongside owner/verified/tags. */}
          <span className="detail-scope">{page.space}</span>
        </div>

        <h2 className="detail-title"><RichTitle title={page.title} /></h2>
        <div className="detail-line">
          <StatusWord status={page.status} />
          {page.last_verified ? <span> · verified {page.last_verified}</span> : null}
        </div>

        {warnings.length > 0 ? (
          <p className="detail-warning" role="alert">
            {warnings.map((w) => `${w.rule}: ${w.detail}`).join(" · ")}
          </p>
        ) : null}

        {visibleClaims.length > 0 ? (
          <>
            <div className="detail-section-title">Claims</div>
            <ul className="claim-ledger">
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
          </>
        ) : null}

        <div className="detail-body">
          {blocks.map((block, index) => {
            if (block.kind === "head") return <h3 key={index}>{block.text}</h3>;
            if (block.kind === "para") return <p key={index}>{block.text}</p>;
            if (block.kind === "code")
              return (
                <pre key={index}>
                  <code>{block.text}</code>
                </pre>
              );
            return (
              <ul key={index}>
                {block.items.map((item, i) => (
                  <li key={i}>{item}</li>
                ))}
              </ul>
            );
          })}
        </div>

        <dl className="detail-meta">
          <dt>Owner</dt>
          <dd>{page.owner ?? "—"}</dd>
          <dt>Verified</dt>
          <dd>{page.last_verified ?? "—"}</dd>
          <dt>Scope</dt>
          <dd>{page.scope}</dd>
          <dt>Tags</dt>
          <dd className="detail-tags">
            {page.tags.map((tag) => (
              <span key={tag} className="tag-chip">
                {tag}
              </span>
            ))}
          </dd>
          {page.sources.length > 0 ? (
            <>
              <dt>Sources</dt>
              <dd>
                {page.sources.map((s) => (
                  <div key={s}>{s}</div>
                ))}
              </dd>
            </>
          ) : null}
        </dl>

        {Object.entries(page.relations)
          .filter(([, items]) => items.length > 0)
          .map(([role, items]) => (
            <div key={role} className="detail-relations">
              <div className="detail-section-title">{ROLE_LABELS[role] ?? role}</div>
              {items.map((item) => (
                <button key={item.slug} type="button" className="detail-relation">
                  <RichTitle title={item.title} />
                </button>
              ))}
            </div>
          ))}

        {page.backlinks.length > 0 ? (
          <div className="detail-relations">
            <div className="detail-section-title">Backlinks</div>
            {page.backlinks.map((item) => (
              <button key={item.slug} type="button" className="detail-relation">
                <RichTitle title={item.title} />
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </article>
  );
}
