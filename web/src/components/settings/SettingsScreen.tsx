import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { plural } from "../../lib/plural";
import { ApiError, createScope, fetchSettings } from "../../lib/api";
import type { SettingsResponse } from "../../lib/types";
import { PencilSimple, Plus } from "@phosphor-icons/react";
import { EmptyState } from "../ui/EmptyState";
import "../../styles/settings.css";

// The Settings screen (02-ui-design-v1.md section 5.6; layout, tabs and card structure from
// /tmp/balise-design/knowledge-v3.html lines 470-615). Five tabs — Structure, Scopes, Sources
// and storage, Models, Export (the mockup's own literal label for the third tab is "Sources
// and storage", used verbatim) — each showing real numbers straight off /api/settings.
// Nothing here computes a figure the server did not already aggregate.
//
// Out of scope, on purpose: every "Add"/"Edit" affordance the mockup shows is a real, visible,
// disabled control with an explanatory title — the same honest-stub precedent AgentsScreen's
// Connect/Edit tabs set. Types, tags, relations and scopes are all defined in on-disk YAML and
// loaded once when `balise serve` starts; there is no in-app editor for any of them, and this
// screen does not pretend otherwise. Export is the sharpest case: there is no `balise export`
// command and no redaction engine in this build, so every Export control is disabled and the
// panel says so in one plain sentence rather than rendering a "preview" that would not really
// redact anything.
//
// Copy rule (02 section 8): never say "uid", "hook", "pack", "index", "token", "budget" or
// "trait" on screen — "agent", "page", "space", "source" and "claim" only. Tooltip text on the
// disabled controls is the only place a literal CLI command appears, same reasoning as
// AgentsScreen: a tooltip is read as a command, not as prose.

type Tab = "structure" | "scopes" | "storage" | "models" | "export";

const TABS: { id: Tab; label: string }[] = [
  { id: "structure", label: "Structure" },
  { id: "scopes", label: "Scopes" },
  { id: "storage", label: "Sources and storage" },
  { id: "models", label: "Models" },
  { id: "export", label: "Export" },
];

const TYPES_TITLE =
  "Types are defined in defaults/types/*.yaml and loaded once when balise serve starts; there is no in-app editor yet.";
// Scopes are add-only: a new one can be declared from this screen (see ScopesTab below), but
// renaming or removing an existing one is not offered, on purpose — renaming would orphan
// every page already filed under the old name, and deleting is destructive. This title makes
// that an honest, explained limit rather than a silently missing feature.
const SCOPES_EDIT_TITLE =
  "Scopes can only be added, never renamed or removed — renaming would orphan every page filed under the old name, and deleting is destructive.";
const EXPORT_TITLE =
  "Export has no redaction engine in this build. There is nothing here that can run yet.";

// Reimplemented rather than imported — this codebase's established convention (AgentsScreen,
// ReviewScreen, Home: none of them share helpers with another screen) is a small local copy
// per screen rather than a shared utility module for a handful of one-line formatters.
function joinWithAnd(items: string[]): string {
  if (items.length === 0) return "";
  if (items.length === 1) return items[0];
  if (items.length === 2) return `${items[0]} and ${items[1]}`;
  return `${items.slice(0, -1).join(", ")}, and ${items[items.length - 1]}`;
}

function staleLabel(days: number): string {
  return days > 0 ? `Stale after ${plural(days, "day")}` : "No staleness rule";
}


function readByLabel(agents: string[]): string {
  return agents.length ? `Read by ${joinWithAnd(agents)}.` : "Not read by any agent yet.";
}


export function SettingsScreen() {
  const [settings, setSettings] = useState<SettingsResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("structure");

  // Pulled out of the mount-only effect so ScopesTab can call it again after a successful
  // create: the create response only carries the new scope's name and the full name list
  // (CreateScopeResponse), not the folder/is_client/count/agents fields a settings row needs
  // to render — a real refetch is what actually completes the picture, live, no restart.
  const loadSettings = useCallback(() => {
    return fetchSettings()
      .then((res) => {
        setLoadError(null);
        setSettings(res);
      })
      .catch((err) => {
        setLoadError(err instanceof Error ? err.message : "Could not load settings.");
      });
  }, []);

  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  if (loadError) {
    return (
      <EmptyState
        title="Could not reach the server."
        hint={`${loadError}. Is balise serve running?`}
      />
    );
  }

  if (!settings) return null;

  return (
    <div className="settings-screen">
      <nav className="settings-nav segmented" role="tablist" aria-label="Settings sections">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className="settings-tab"
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </nav>

      {/* Keyed on the tab so the one entrance animation replays when the section changes. */}
      <div className="settings-content" key={tab}>
        {tab === "structure" ? <StructureTab settings={settings} /> : null}
        {tab === "scopes" ? <ScopesTab settings={settings} onScopeCreated={loadSettings} /> : null}
        {tab === "storage" ? <StorageTab settings={settings} /> : null}
        {tab === "models" ? <ModelsTab settings={settings} /> : null}
        {tab === "export" ? <ExportTab settings={settings} /> : null}
      </div>
    </div>
  );
}

function CardHead({
  id,
  title,
  count,
  children,
}: {
  id: string;
  title: string;
  count?: number;
  children?: ReactNode;
}) {
  return (
    <div className="settings-card-head">
      <h2 id={id}>{title}</h2>
      {count !== undefined ? <span className="count">{count}</span> : null}
      {children}
    </div>
  );
}

function EditStub({ title }: { title: string }) {
  return (
    <button type="button" className="btn btn-quiet btn-sm settings-edit" disabled title={title}>
      <PencilSimple size={14} aria-hidden="true" />
      Edit
    </button>
  );
}

function FieldList({ fields }: { fields: string[] }) {
  if (fields.length === 0) return <span>No extra fields</span>;
  return (
    <span>
      Fields:{" "}
      {fields.map((f, i) => (
        <span key={f}>
          <code className="settings-mono">{f}</code>
          {i < fields.length - 1 ? ", " : null}
        </span>
      ))}
    </span>
  );
}

function StructureTab({ settings }: { settings: SettingsResponse }) {
  return (
    <>
      <PageTypesCard settings={settings} />
      <TagsCard settings={settings} />
      <RelationsCard settings={settings} />
    </>
  );
}

function PageTypesCard({ settings }: { settings: SettingsResponse }) {
  return (
    <section className="settings-card panel" aria-labelledby="settings-types-heading">
      <CardHead id="settings-types-heading" title="Page types" count={settings.types.length}>
        <button
          type="button"
          className="btn btn-secondary btn-sm settings-add"
          disabled
          title={TYPES_TITLE}
        >
          <Plus size={14} aria-hidden="true" />
          Add a type
        </button>
      </CardHead>
      <p className="settings-card-desc">
        A type says what a page is, which fields it carries, and how long before it counts as
        stale.
      </p>
      <ul className="settings-rows">
        {settings.types.map((t) => (
          <li key={t.name} className="settings-row settings-type-row">
            <span className="settings-dot" data-type={t.name} aria-hidden="true" />
            <div className="settings-row-body">
              <div className="settings-row-title">
                <span className="settings-row-name">{t.name}</span>
                <span className="count">{plural(t.count, "page")}</span>
              </div>
              {t.description ? <p className="settings-row-desc">{t.description}</p> : null}
              <p className="settings-row-sub">
                <FieldList fields={t.fields} />
                <span aria-hidden="true"> · </span>
                <span>{staleLabel(t.stale_after_days)}</span>
              </p>
            </div>
            <EditStub title={TYPES_TITLE} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function TagsCard({ settings }: { settings: SettingsResponse }) {
  return (
    <section className="settings-card panel" aria-labelledby="settings-tags-heading">
      <CardHead id="settings-tags-heading" title="Tags" count={settings.tag_groups.length} />
      <p className="settings-card-desc">
        Tags group pages by facet. Groups come from <code className="settings-mono">defaults/facets/</code>;
        values are however pages have actually been tagged.
      </p>
      <ul className="settings-rows">
        {settings.tag_groups.map((g) => (
          <li key={g.name} className="settings-row settings-row-compact">
            <span className="settings-row-name">{g.name}</span>
            <span className="settings-row-meta">{plural(g.top_level_count, "value")}</span>
          </li>
        ))}
      </ul>
      {settings.tags.length > 0 ? (
        <>
          <h3 className="settings-subhead">In use</h3>
          <ul className="settings-tag-chips">
            {settings.tags.map((tag) => (
              <li key={tag.name} className="settings-tag-chip">
                <span className="settings-tag-chip-name">{tag.name}</span>
                <span className="settings-tag-chip-count">{tag.count}</span>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p className="settings-empty-note">No page has been tagged yet.</p>
      )}
    </section>
  );
}

function RelationsCard({ settings }: { settings: SettingsResponse }) {
  return (
    <section className="settings-card panel" aria-labelledby="settings-relations-heading">
      <CardHead
        id="settings-relations-heading"
        title="Relations"
        count={settings.relations.length}
      />
      <p className="settings-card-desc">
        A relation connects two pages. Each kind below is in real use somewhere in this vault.
      </p>
      {settings.relations.length > 0 ? (
        <ul className="settings-rows">
          {settings.relations.map((r) => (
            <li key={r.kind} className="settings-row settings-row-compact">
              <span className="settings-row-name settings-mono">{r.kind}</span>
              <span className="settings-row-meta">{plural(r.count, "edge")}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="settings-empty-note">No page links to another one yet.</p>
      )}
    </section>
  );
}

function ScopesTab({
  settings,
  onScopeCreated,
}: {
  settings: SettingsResponse;
  onScopeCreated: () => void;
}) {
  const [showAddForm, setShowAddForm] = useState(false);

  return (
    <section className="settings-card panel" aria-labelledby="settings-scopes-heading">
      <CardHead id="settings-scopes-heading" title="Scopes" count={settings.scopes.length}>
        <button
          type="button"
          className={`btn btn-sm settings-add ${showAddForm ? "btn-quiet" : "btn-secondary"}`}
          aria-expanded={showAddForm}
          onClick={() => setShowAddForm((open) => !open)}
        >
          {showAddForm ? null : <Plus size={14} aria-hidden="true" />}
          {showAddForm ? "Cancel" : "Add a scope"}
        </button>
      </CardHead>
      <p className="settings-card-desc">
        A scope is a top-level folder. An agent is given whole scopes to read; nothing crosses
        between them.
      </p>

      {/* Unmounting the form on close is what discards a half-typed name: it owns its state. */}
      {showAddForm ? (
        <AddScopeForm
          onClose={() => setShowAddForm(false)}
          onCreated={() => {
            setShowAddForm(false);
            onScopeCreated();
          }}
        />
      ) : null}

      <ul className="settings-rows">
        {settings.scopes.map((s) => (
          <li key={s.name} className="settings-row">
            <span
              className={`settings-scope-dot${s.is_client ? " is-client" : ""}`}
              aria-hidden="true"
            />
            <div className="settings-row-body">
              <div className="settings-row-title">
                <span className="settings-row-name">{s.name}</span>
                <span className="settings-mono settings-row-folder">{s.folder}</span>
                <span className="count">{plural(s.count, "page")}</span>
              </div>
              <p className="settings-row-sub">{readByLabel(s.agents)}</p>
            </div>
            <EditStub title={SCOPES_EDIT_TITLE} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function AddScopeForm({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Name the scope before adding it.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await createScope(trimmed);
      onCreated();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Could not add this scope. Try again.");
      setBusy(false);
    }
  }

  return (
    <form className="settings-add-form" onSubmit={handleSubmit} noValidate>
      <div className="settings-form-field">
        <label htmlFor="settings-add-scope-name">Name</label>
        <input
          id="settings-add-scope-name"
          className="field settings-mono"
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={100}
          placeholder="e.g. ops…"
          aria-describedby="settings-add-scope-help"
          aria-invalid={error ? true : undefined}
        />
        <p id="settings-add-scope-help" className="settings-form-help">
          Becomes a top-level folder of the same name. It cannot be renamed later.
        </p>
      </div>
      {error ? (
        <p className="settings-form-error" role="alert">
          {error}
        </p>
      ) : null}
      <div className="settings-form-actions">
        <button type="submit" className="btn btn-primary btn-sm" disabled={busy}>
          {busy ? "Adding…" : "Add scope"}
        </button>
        <button type="button" className="btn btn-quiet btn-sm" onClick={onClose}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function DefRow({ label, title, children }: { label: string; title?: string; children: ReactNode }) {
  return (
    <div className="settings-def-row">
      <dt>{label}</dt>
      <dd title={title}>{children}</dd>
    </div>
  );
}

function Unset({ children = "Not set" }: { children?: ReactNode }) {
  return <span className="settings-unset">{children}</span>;
}

function StorageTab({ settings }: { settings: SettingsResponse }) {
  const { sources } = settings;
  return (
    <section className="settings-card panel" aria-labelledby="settings-storage-heading">
      <CardHead id="settings-storage-heading" title="Sources and storage" />
      <p className="settings-card-desc">
        Where this vault's pages actually live. Read-only: change it by restarting
        balise serve with different flags, not from this screen.
      </p>
      <dl className="settings-def-list">
        <DefRow label="Vault" title="Set by the path balise serve was started with.">
          <span className="settings-mono settings-def-value">{sources.vault_path}</span>
          <span className="settings-def-note">
            {sources.is_git_repo ? "git repository" : "not a git repository"}
          </span>
        </DefRow>
        <DefRow label="Pages">{plural(sources.page_count, "page")}</DefRow>
        <DefRow label="History">
          {sources.is_git_repo ? (
            plural(sources.commit_count, "commit")
          ) : (
            <Unset>No commit history</Unset>
          )}
        </DefRow>
        <DefRow label="Database" title="Set by BALISE_DSN. Never shows a password.">
          <span className="settings-mono settings-def-value">
            {sources.db_host} / {sources.db_name}
          </span>
        </DefRow>
      </dl>
      <p className="settings-empty-note">
        Attachment storage and commit-author mapping are not configured in this build.
      </p>
    </section>
  );
}

function ModelsTab({ settings }: { settings: SettingsResponse }) {
  const { models } = settings;
  return (
    <section className="settings-card panel" aria-labelledby="settings-models-heading">
      <CardHead id="settings-models-heading" title="Models" />
      <p className="settings-card-desc">
        Which gateway an agent uses to extract claims from a proposal. Read-only: set by
        environment variables when balise serve starts.
      </p>
      <dl className="settings-def-list">
        <DefRow label="Claude gateway" title="Set by ANTHROPIC_BASE_URL.">
          {models.gateway_url_set ? (
            <span className="settings-mono settings-def-value">{models.gateway_url}</span>
          ) : (
            <Unset />
          )}
        </DefRow>
        <DefRow label="API key" title="Set by ANTHROPIC_API_KEY. Never shown here.">
          {models.api_key_set ? "Set" : <Unset />}
        </DefRow>
      </dl>
      <p className="settings-empty-note">{models.note}</p>
    </section>
  );
}

function ExportTab({ settings }: { settings: SettingsResponse }) {
  return (
    <section className="settings-card panel" aria-labelledby="settings-export-heading">
      <CardHead id="settings-export-heading" title="Export a scope" />
      <p className="settings-card-desc settings-export-note">
        Export does not exist yet in this build. There is no redaction engine, so a preview
        here would not really redact anything.
      </p>
      <div className="settings-export-grid">
        <div className="settings-form-field">
          <label htmlFor="settings-export-scope">Scope</label>
          <select
            id="settings-export-scope"
            className="field"
            disabled
            title={EXPORT_TITLE}
            defaultValue=""
          >
            <option value="" disabled>
              Choose a scope…
            </option>
            {settings.scopes.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="settings-form-field">
          <label htmlFor="settings-export-audience">Audience</label>
          <select
            id="settings-export-audience"
            className="field"
            disabled
            title={EXPORT_TITLE}
            defaultValue=""
          >
            <option value="" disabled>
              Choose an audience…
            </option>
          </select>
        </div>
      </div>
      <div className="settings-form-actions">
        <button type="button" className="btn btn-primary" disabled title={EXPORT_TITLE}>
          Export
        </button>
        <button type="button" className="btn btn-secondary" disabled title={EXPORT_TITLE}>
          Preview redaction
        </button>
      </div>
    </section>
  );
}
