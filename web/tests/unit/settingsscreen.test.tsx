import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SettingsScreen } from "../../src/components/settings/SettingsScreen";
import type { SettingsResponse } from "../../src/lib/types";

// SettingsScreen owns its own /api/settings fetching (AgentsScreen/SourcesScreen's
// self-contained-data-fetching pattern), so these tests stub the global fetch rather than
// mocking the api module.

const SETTINGS: SettingsResponse = {
  types: [
    {
      name: "incident",
      description: "Something broke and someone had to respond.",
      folder: "incidents",
      count: 4,
      fields: ["severity", "resolved_at"],
      stale_after_days: 0,
    },
    {
      name: "entity",
      description: "A person, team, vendor or system this vault talks about.",
      folder: "entities",
      count: 12,
      fields: ["vendor", "owner"],
      stale_after_days: 365,
    },
  ],
  tag_groups: [
    { name: "customer", top_level_count: 5 },
    { name: "layer", top_level_count: 4 },
    { name: "vendor", top_level_count: 3 },
  ],
  tags: [
    { name: "customer/acme", count: 7 },
    { name: "layer/api", count: 3 },
  ],
  relations: [{ kind: "about", count: 9 }],
  scopes: [
    { name: "work", folder: "work", is_client: false, count: 79, agents: ["Claude Code"] },
    { name: "client-globex", folder: "client-globex", is_client: true, count: 42, agents: [] },
  ],
  sources: {
    vault_path: "/tmp/balise-live",
    is_git_repo: true,
    db_host: "localhost",
    db_name: "balise",
    page_count: 139,
    commit_count: 27,
  },
  models: {
    gateway_url: "",
    gateway_url_set: false,
    api_key_set: false,
    note: "Each proposal names the model it used when it was created.",
  },
};

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body };
}

function mockFetch(body: unknown = SETTINGS, status = 200) {
  return vi.fn(async () => jsonResponse(body, status));
}

// The settings row a freshly-created "ops" scope would refetch as: zero pages, zero agents —
// same honesty as any other brand-new, empty scope (see personal/agentsscreen.test.tsx's own
// SETTINGS_WITH_SPACES comment on why an empty one still has to render).
const SETTINGS_WITH_NEW_SCOPE: SettingsResponse = {
  ...SETTINGS,
  scopes: [...SETTINGS.scopes, { name: "ops", folder: "ops", is_client: false, count: 0, agents: [] }],
};

// Method-aware mock covering GET /api/settings and POST /api/scopes, so the Add-scope
// happy path's real refetch-after-create (SettingsScreen's loadSettings, not a locally
// patched-in row) can be exercised end to end.
function mockWriteFetch() {
  let created = false;
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (method === "GET" && url.endsWith("/api/settings")) {
      return jsonResponse(created ? SETTINGS_WITH_NEW_SCOPE : SETTINGS);
    }
    if (method === "POST" && url.endsWith("/api/scopes")) {
      const body = JSON.parse(String(init?.body ?? "{}"));
      if (!body.name) return jsonResponse({ detail: "scope name is required" }, 400);
      if (body.name === "work") return jsonResponse({ detail: "scope already exists" }, 409);
      created = true;
      return jsonResponse({ name: body.name, scopes: ["work", "client-globex", body.name] }, 201);
    }
    throw new Error(`unexpected fetch: ${method} ${url}`);
  });
}

beforeEach(() => vi.restoreAllMocks());

describe("SettingsScreen", () => {
  it("renders real per-type counts and staleness, never a fabricated placeholder", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<SettingsScreen />);
    expect(await screen.findByText("incident")).toBeTruthy();
    expect(screen.getByText("4 pages")).toBeTruthy();
    expect(screen.getByText("entity")).toBeTruthy();
    expect(screen.getByText("12 pages")).toBeTruthy();
    expect(screen.getByText(/no staleness rule/i)).toBeTruthy();
    expect(screen.getByText(/stale after 365 days/i)).toBeTruthy();
  });

  it("renders real facet groups and tag usage counts", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<SettingsScreen />);
    await screen.findByText("incident");
    expect(screen.getByText("customer")).toBeTruthy();
    expect(screen.getByText("5 values")).toBeTruthy();
    expect(screen.getByText("customer/acme")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
  });

  it("ships Add a type / Edit as honestly-labelled disabled stubs", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<SettingsScreen />);
    await screen.findByText("incident");
    const addType = screen.getByRole("button", { name: "Add a type" });
    expect(addType.hasAttribute("disabled")).toBe(true);
    expect(addType.getAttribute("title")).toMatch(/defaults\/types\/\*\.yaml/);
    const edits = screen.getAllByRole("button", { name: "Edit" });
    expect(edits.length).toBeGreaterThan(0);
    for (const btn of edits) expect(btn.hasAttribute("disabled")).toBe(true);
  });

  it("shows real scope counts and an honest 'not read by any agent' line", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Scopes" }));
    // "work" is both the scope name and its folder (folder == scope name for a top-level
    // scope), so it legitimately renders twice — assert on the count rather than uniqueness.
    expect((await screen.findAllByText("work")).length).toBe(2);
    expect(screen.getByText("79 pages")).toBeTruthy();
    expect(screen.getByText(/read by claude code/i)).toBeTruthy();
    expect(screen.getByText(/not read by any agent yet/i)).toBeTruthy();
    // Unlike Add a type (a genuine, permanently disabled stub), Add a scope is a real,
    // enabled affordance — see the dedicated Add-scope tests below for its form behaviour.
    const addScope = screen.getByRole("button", { name: "Add a scope" });
    expect(addScope.hasAttribute("disabled")).toBe(false);
  });

  it("adds a scope and shows it live, with no restart, once the create resolves", async () => {
    vi.stubGlobal("fetch", mockWriteFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Scopes" }));
    await screen.findAllByText("work");

    await user.click(screen.getByRole("button", { name: "Add a scope" }));
    await user.type(screen.getByLabelText("Name"), "ops");
    await user.click(screen.getByRole("button", { name: "Add scope" }));

    // The form closes and the new scope appears via a real GET /api/settings refetch (the
    // create response alone doesn't carry folder/count/agents — see loadSettings's comment).
    // "ops" is both the scope name and its folder (folder == scope name for a top-level
    // scope, same as "work" above), so it legitimately renders twice.
    expect((await screen.findAllByText("ops")).length).toBe(2);
    expect(screen.queryByLabelText("Name")).toBeNull();
  });

  it("requires a name before submitting the Add-scope form, with no network call made", async () => {
    const fetchMock = mockWriteFetch();
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Scopes" }));
    await screen.findAllByText("work");

    await user.click(screen.getByRole("button", { name: "Add a scope" }));
    const callsBefore = fetchMock.mock.calls.length;
    await user.click(screen.getByRole("button", { name: "Add scope" }));

    expect(await screen.findByText(/name the scope/i)).toBeTruthy();
    expect(fetchMock.mock.calls.length).toBe(callsBefore);
  });

  it("surfaces a server-rejected duplicate scope name as an announced error", async () => {
    vi.stubGlobal("fetch", mockWriteFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Scopes" }));
    await screen.findAllByText("work");

    await user.click(screen.getByRole("button", { name: "Add a scope" }));
    await user.type(screen.getByLabelText("Name"), "work");
    await user.click(screen.getByRole("button", { name: "Add scope" }));

    const error = await screen.findByText(/scope already exists/i);
    expect(error.getAttribute("role")).toBe("alert");
    // The form stays open on failure, so the name can be corrected rather than retyped.
    expect(screen.getByLabelText("Name")).toBeTruthy();
  });

  it("closing the Add-scope form after typing a name discards it, not just hides it", async () => {
    vi.stubGlobal("fetch", mockWriteFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Scopes" }));
    await screen.findAllByText("work");

    await user.click(screen.getByRole("button", { name: "Add a scope" }));
    await user.type(screen.getByLabelText("Name"), "ops");
    // Two "Cancel"-labelled buttons exist while the form is open: the toggle itself (which
    // reads "Cancel" instead of "Add a scope" when expanded) and the form's own Cancel button
    // — the form's is the one added later in the DOM.
    const cancelButtons = screen.getAllByRole("button", { name: "Cancel" });
    await user.click(cancelButtons[cancelButtons.length - 1]);
    expect(screen.queryByLabelText("Name")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Add a scope" }));
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("");
  });

  it("shows real vault/db facts on Sources and storage, and never MinIO", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Sources and storage" }));
    expect(await screen.findByDisplayValue(/\/tmp\/balise-live \(git\)/)).toBeTruthy();
    expect(screen.getByDisplayValue("139 pages")).toBeTruthy();
    expect(screen.getByDisplayValue("27 commits")).toBeTruthy();
    expect(screen.getByDisplayValue("localhost / balise")).toBeTruthy();
    expect(document.body.innerHTML).not.toMatch(/minio/i);
  });

  it("reports an unset gateway/API key honestly on Models, never a fabricated model name", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Models" }));
    // Both the gateway and the API key fields are unset in this fixture, so "Not set" is
    // expected to appear twice — disambiguate by each field's own explanatory title instead.
    const notSet = await screen.findAllByDisplayValue("Not set");
    expect(notSet.length).toBe(2);
    expect(screen.getByTitle(/ANTHROPIC_BASE_URL/)).toBeTruthy();
    expect(screen.getByTitle(/ANTHROPIC_API_KEY/)).toBeTruthy();
    expect(document.body.innerHTML).not.toMatch(/bge-m3|bge-reranker/i);
  });

  it("ships Export as a fully disabled stub with one honest sentence", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    await user.click(screen.getByRole("tab", { name: "Export" }));
    expect(await screen.findByText(/export does not exist yet in this build/i)).toBeTruthy();
    const preview = screen.getByRole("button", { name: "Preview redaction" });
    const exportBtn = screen.getByRole("button", { name: "Export" });
    expect(preview.hasAttribute("disabled")).toBe(true);
    expect(exportBtn.hasAttribute("disabled")).toBe(true);
    expect(preview.getAttribute("title")).toMatch(/no redaction engine/i);
  });

  it("never renders a password, DB user, token hash, or the words uid/hook/pack/index/budget/trait", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<SettingsScreen />);
    await screen.findByText("incident");
    for (const tabName of ["Scopes", "Sources and storage", "Models", "Export"]) {
      await user.click(screen.getByRole("tab", { name: tabName }));
    }
    const html = document.body.innerHTML.toLowerCase();
    expect(html).not.toContain("password");
    expect(html).not.toContain("token_hash");
    expect(html).not.toContain("db_user");
    expect(html).not.toMatch(/\buid\b/);
    expect(html).not.toMatch(/\bhook\b/);
    expect(html).not.toMatch(/\bpack\b/);
    expect(html).not.toMatch(/\bindex\b/);
    expect(html).not.toMatch(/\bbudget\b/);
    expect(html).not.toMatch(/\btrait\b/);
  });

  it("shows a server-unreachable empty state on fetch failure", async () => {
    vi.stubGlobal("fetch", mockFetch({ detail: "boom" }, 500));
    render(<SettingsScreen />);
    expect(await screen.findByText(/could not reach the server/i)).toBeTruthy();
  });
});
