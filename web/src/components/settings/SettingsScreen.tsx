import { useCallback, useEffect, useState, type FormEvent } from "react";
import { plural } from "../../lib/plural";
import { ApiError, createScope, fetchSettings } from "../../lib/api";
import type { SettingsResponse } from "../../lib/types";
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

function fieldsLabel(fields: string[]): string {
  return fields.length ? `Fields: ${fields.join(", ")}` : "No extra fields";
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
      <nav className="settings-nav" role="tablist" aria-label="Settings sections">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className={`settings-tab${tab === t.id ? " is-active" : ""}`}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </nav>

      <div className="settings-content">
        {tab === "structure" ? <StructureTab settings={settings} /> : null}
        {tab === "scopes" ? <ScopesTab settings={settings} onScopeCreated={loadSettings} /> : null}
        {tab === "storage" ? <StorageTab settings={settings} /> : null}
        {tab === "models" ? <ModelsTab settings={settings} /> : null}
        {tab === "export" ? <ExportTab settings={settings} /> : null}
      </div>
    </div>
  );
}

function StructureTab({ settings }: { settings: SettingsResponse }) {
  return (
    <>
      <section className="settings-card" aria-labelledby="settings-types-heading">
        <div className="settings-card-head">
          <h2 id="settings-types-heading">Page types</h2>
          <span className="settings-card-count">{settings.types.length}</span>
          <button type="button" className="settings-add" disabled title={TYPES_TITLE}>
            Add a type
          </button>
        </div>
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
                  <span className="settings-row-count">{plural(t.count, "page")}</span>
                </div>
                {t.description ? <p className="settings-row-desc">{t.description}</p> : null}
                <p className="settings-row-sub">
                  {fieldsLabel(t.fields)} · {staleLabel(t.stale_after_days)}
                </p>
              </div>
              <button
                type="button"
                className="settings-edit"
                disabled
                title={TYPES_TITLE}
              >
                Edit
              </button>
            </li>
          ))}
        </ul>
      </section>

      <section className="settings-card" aria-labelledby="settings-tags-heading">
        <div className="settings-card-head">
          <h2 id="settings-tags-heading">Tags</h2>
          <span className="settings-card-count">{settings.tag_groups.length}</span>
        </div>
        <p className="settings-card-desc">
          Tags group pages by facet. Groups come from defaults/facets/; values are however
          pages have actually been tagged.
        </p>
        <ul className="settings-rows">
          {settings.tag_groups.map((g) => (
            <li key={g.name} className="settings-row">
              <div className="settings-row-body">
                <div className="settings-row-title">
                  <span className="settings-row-name">{g.name}</span>
                  <span className="settings-row-count">{plural(g.top_level_count, "value")}</span>
                </div>
              </div>
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

      <section className="settings-card" aria-labelledby="settings-relations-heading">
        <div className="settings-card-head">
          <h2 id="settings-relations-heading">Relations</h2>
          <span className="settings-card-count">{settings.relations.length}</span>
        </div>
        <p className="settings-card-desc">
          A relation connects two pages. Each kind below is in real use somewhere in this vault.
        </p>
        {settings.relations.length > 0 ? (
          <ul className="settings-rows">
            {settings.relations.map((r) => (
              <li key={r.kind} className="settings-row">
                <div className="settings-row-body">
                  <div className="settings-row-title">
                    <span className="settings-row-name settings-mono">{r.kind}</span>
                    <span className="settings-row-count">{plural(r.count, "edge")}</span>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="settings-empty-note">No page links to another one yet.</p>
        )}
      </section>
    </>
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
  const [addName, setAddName] = useState("");
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);

  function openAddForm() {
    setAddName("");
    setAddError(null);
    setShowAddForm(true);
  }

  function closeAddForm() {
    setShowAddForm(false);
    setAddName("");
    setAddError(null);
  }

  async function handleAddSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = addName.trim();
    if (!name) {
      setAddError("Name the scope before adding it.");
      return;
    }
    setAddBusy(true);
    setAddError(null);
    try {
      await createScope(name);
      closeAddForm();
      onScopeCreated();
    } catch (err) {
      setAddError(err instanceof ApiError ? err.message : "Could not add this scope. Try again.");
    } finally {
      setAddBusy(false);
    }
  }

  return (
    <section className="settings-card" aria-labelledby="settings-scopes-heading">
      <div className="settings-card-head">
        <h2 id="settings-scopes-heading">Scopes</h2>
        <span className="settings-card-count">{settings.scopes.length}</span>
        <button
          type="button"
          className="settings-add"
          aria-expanded={showAddForm}
          onClick={() => (showAddForm ? closeAddForm() : openAddForm())}
        >
          {showAddForm ? "Cancel" : "Add a scope"}
        </button>
      </div>
      <p className="settings-card-desc">
        A scope is a top-level folder. An agent is given whole scopes to read; nothing crosses
        between them.
      </p>

      {showAddForm ? (
        <form className="settings-add-form" onSubmit={handleAddSubmit}>
          <div className="settings-form-field">
            <label htmlFor="settings-add-scope-name">Name</label>
            <input
              id="settings-add-scope-name"
              type="text"
              value={addName}
              onChange={(e) => setAddName(e.target.value)}
              maxLength={100}
            />
          </div>
          {addError ? (
            <p className="settings-form-error" role="alert">
              {addError}
            </p>
          ) : null}
          <div className="settings-form-actions">
            <button type="submit" className="settings-form-submit" disabled={addBusy}>
              {addBusy ? "Adding…" : "Add scope"}
            </button>
            <button type="button" className="settings-form-cancel" onClick={closeAddForm}>
              Cancel
            </button>
          </div>
        </form>
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
                <span className="settings-row-count">{plural(s.count, "page")}</span>
              </div>
              <p className="settings-row-sub">{readByLabel(s.agents)}</p>
            </div>
            <button type="button" className="settings-edit" disabled title={SCOPES_EDIT_TITLE}>
              Edit
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function StorageTab({ settings }: { settings: SettingsResponse }) {
  const { sources } = settings;
  const gitLabel = sources.is_git_repo ? "git" : "not a git repository";
  const commitsLabel = sources.is_git_repo
    ? plural(sources.commit_count, "commit")
    : "No commit history (not a git repository)";
  return (
    <section className="settings-card" aria-labelledby="settings-storage-heading">
      <div className="settings-card-head">
        <h2 id="settings-storage-heading">Sources and storage</h2>
      </div>
      <p className="settings-card-desc">
        Where this vault's pages actually live. Read-only: change it by restarting
        balise serve with different flags, not from this screen.
      </p>
      <dl className="settings-field-grid">
        <div className="settings-field-row">
          <dt>Vault</dt>
          <dd>
            <input
              className="settings-field-value"
              value={`${sources.vault_path} (${gitLabel})`}
              disabled
              readOnly
              title="Set by the path balise serve was started with."
            />
          </dd>
        </div>
        <div className="settings-field-row">
          <dt>Pages</dt>
          <dd>
            <input
              className="settings-field-value"
              value={plural(sources.page_count, "page")}
              disabled
              readOnly
            />
          </dd>
        </div>
        <div className="settings-field-row">
          <dt>History</dt>
          <dd>
            <input className="settings-field-value" value={commitsLabel} disabled readOnly />
          </dd>
        </div>
        <div className="settings-field-row">
          <dt>Database</dt>
          <dd>
            <input
              className="settings-field-value"
              value={`${sources.db_host} / ${sources.db_name}`}
              disabled
              readOnly
              title="Set by BALISE_DSN. Never shows a password."
            />
          </dd>
        </div>
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
    <section className="settings-card" aria-labelledby="settings-models-heading">
      <div className="settings-card-head">
        <h2 id="settings-models-heading">Models</h2>
      </div>
      <p className="settings-card-desc">
        Which gateway an agent uses to extract claims from a proposal. Read-only: set by
        environment variables when balise serve starts.
      </p>
      <dl className="settings-field-grid">
        <div className="settings-field-row">
          <dt>Claude gateway</dt>
          <dd>
            <input
              className="settings-field-value"
              value={models.gateway_url_set ? models.gateway_url : "Not set"}
              disabled
              readOnly
              title="Set by ANTHROPIC_BASE_URL."
            />
          </dd>
        </div>
        <div className="settings-field-row">
          <dt>API key</dt>
          <dd>
            <input
              className="settings-field-value"
              value={models.api_key_set ? "Set" : "Not set"}
              disabled
              readOnly
              title="Set by ANTHROPIC_API_KEY. Never shown here."
            />
          </dd>
        </div>
      </dl>
      <p className="settings-empty-note">{models.note}</p>
    </section>
  );
}

function ExportTab({ settings }: { settings: SettingsResponse }) {
  return (
    <section className="settings-card" aria-labelledby="settings-export-heading">
      <div className="settings-card-head">
        <h2 id="settings-export-heading">Export a scope</h2>
      </div>
      <p className="settings-card-desc settings-export-note">
        Export does not exist yet in this build. There is no redaction engine, so a preview
        here would not really redact anything.
      </p>
      <div className="settings-export-grid">
        <label className="settings-field-row">
          <span>Scope</span>
          <select disabled title={EXPORT_TITLE} defaultValue="">
            <option value="" disabled>
              Choose a scope
            </option>
            {settings.scopes.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <label className="settings-field-row">
          <span>Audience</span>
          <select disabled title={EXPORT_TITLE} defaultValue="">
            <option value="" disabled>
              Choose an audience
            </option>
          </select>
        </label>
      </div>
      <div className="settings-export-actions">
        <button type="button" disabled title={EXPORT_TITLE}>
          Preview redaction
        </button>
        <button type="button" disabled title={EXPORT_TITLE}>
          Export
        </button>
      </div>
    </section>
  );
}
