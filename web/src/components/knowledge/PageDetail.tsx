import { ArrowLeft } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { parseBody } from "../../lib/markdown";
import type { Block } from "../../lib/markdown";
import type { PageDetail } from "../../lib/types";
import { EmptyState } from "../ui/EmptyState";
import { RichTitle } from "../ui/RichTitle";
import { isNotable, StatusWord } from "../ui/StatusWord";
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
// List view renders this in place of the browse grid entirely — Knowledge.tsx only mounts this
// component when a page is open, so `page === null` here always means "still fetching it",
// never "nothing selected".
//
// Order is claims first, then the body, then where it came from. body_md is raw markdown —
// parseBody runs here in the browser; the server never reshapes a page body (`01` §1.2
// principle 3). Title, claims and body share one reading measure so their left and right
// edges line up; the metadata sits beside them on wide screens and below them on narrow ones
// (the grid areas in knowledge.css decide which).
export function PageReader(
  { page, onBack }: { page: PageDetail | null; onBack: () => void },
) {
  if (!page) {
    return (
      <article className="kn-reader" aria-busy="true">
        <div className="kn-reader-inner">
          <BackCrumb onBack={onBack} />
          <div className="kn-reader-main">
            <EmptyState
              title="Loading page…"
              hint="Fetching claims, body, and relations."
            />
          </div>
        </div>
      </article>
    );
  }

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
        <BackCrumb onBack={onBack} />

        <div className="kn-reader-main">
          <div className="detail-top">
            <TypeChip type={page.type} />
            {/* v3 binds this meta line to page.space, not page.scope — scope shows in the
                metadata aside alongside owner/verified/tags. */}
            <span className="detail-scope">{page.space}</span>
            {isNotable(page.status) ? <StatusWord status={page.status} /> : null}
          </div>

          <h2 className="detail-title"><RichTitle title={page.title} /></h2>

          {warnings.length > 0 ? (
            <p className="detail-warning" role="alert">
              {warnings.map((w) => `${w.rule}: ${w.detail}`).join(" · ")}
            </p>
          ) : null}

          {visibleClaims.length > 0 ? (
            <section className="detail-claims" aria-label="Claims">
              <h3 className="detail-section-title">Claims</h3>
              <ol className="claim-ledger">
                {visibleClaims.map((claim, index) => (
                  <li
                    key={index}
                    className="claim-row"
                    data-historical={claim.status !== "active" ? "true" : "false"}
                  >
                    <span className="claim-index" aria-hidden="true">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span className="claim-text">{claim.text}</span>
                    {isNotable(claim.status) ? (
                      <StatusWord status={claim.status} asOf={claim.as_of} />
                    ) : null}
                  </li>
                ))}
              </ol>
            </section>
          ) : null}

          <div className="detail-body">
            {parseBody(page.body_md).map((block, index) => renderBlock(block, index))}
          </div>
        </div>

        <MetaAside page={page} />

        <Relations page={page} />
      </div>
    </article>
  );
}

function BackCrumb({ onBack }: { onBack: () => void }) {
  return (
    <div className="detail-crumb">
      <button type="button" className="btn btn-quiet btn-sm detail-back" onClick={onBack}>
        <ArrowLeft size={16} aria-hidden="true" />
        Back to Knowledge
      </button>
    </div>
  );
}

function renderBlock(block: Block, index: number): ReactNode {
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
}

// Every row always renders, and an empty value reads "Not set" in the same muted style, so a
// missing owner looks deliberate rather than like a rendering gap.
function NotSet() {
  return <span className="detail-unset">Not set</span>;
}

function MetaAside({ page }: { page: PageDetail }) {
  return (
    <aside className="detail-aside" aria-label="Page details">
      <dl className="detail-meta">
        <dt>Owner</dt>
        <dd>{page.owner || <NotSet />}</dd>
        <dt>Verified</dt>
        <dd>{page.last_verified || <NotSet />}</dd>
        <dt>Scope</dt>
        <dd className="detail-mono">{page.scope}</dd>
        <dt>Tags</dt>
        <dd className="detail-tags">
          {page.tags.length > 0
            ? page.tags.map((tag) => (
                <span key={tag} className="tag-chip">
                  {tag}
                </span>
              ))
            : <NotSet />}
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
    </aside>
  );
}

function Relations({ page }: { page: PageDetail }) {
  const roles = Object.entries(page.relations).filter(([, items]) => items.length > 0);
  if (roles.length === 0 && page.backlinks.length === 0) return null;
  return (
    <div className="kn-reader-rel">
      {roles.map(([role, items]) => (
        <RelationGroup key={role} label={ROLE_LABELS[role] ?? role} items={items} />
      ))}
      {page.backlinks.length > 0 ? (
        <RelationGroup label="Backlinks" items={page.backlinks} />
      ) : null}
    </div>
  );
}

function RelationGroup({ label, items }: { label: string; items: { slug: string; title: string }[] }) {
  return (
    <div className="detail-relations">
      <h3 className="detail-section-title">{label}</h3>
      {items.map((item) => (
        <button key={item.slug} type="button" className="detail-relation">
          <RichTitle title={item.title} />
        </button>
      ))}
    </div>
  );
}
