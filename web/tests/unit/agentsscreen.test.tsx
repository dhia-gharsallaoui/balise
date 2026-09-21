import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentsScreen } from "../../src/components/agents/AgentsScreen";
import type { AgentActivityResponse, AgentsListResponse } from "../../src/lib/types";

// AgentsScreen owns its own /api/agents + /api/agents/{id}/activity fetching, the same
// self-contained-data-fetching pattern ReviewScreen uses — so these tests stub the global
// fetch (reviewscreen.test.tsx's convention) rather than mocking the api module.

const AGENTS: AgentsListResponse = {
  agents: [
    {
      id: "a-active",
      name: "Claude Code",
      scopes: ["work", "client-globex"],
      capabilities: ["read", "remember"],
      state: "active",
      created_at: "2026-09-01T00:00:00Z",
      last_used_at: "2026-09-19T09:40:00Z",
    },
    {
      id: "a-revoked",
      name: "Old bot",
      scopes: ["work"],
      capabilities: ["read"],
      state: "revoked",
      created_at: "2026-08-01T00:00:00Z",
      last_used_at: null,
    },
    {
      id: "a-expired",
      name: "Temp helper",
      scopes: ["client-globex"],
      capabilities: ["read"],
      state: "expired",
      created_at: "2026-07-01T00:00:00Z",
      last_used_at: null,
    },
    {
      id: "a-mixed",
      name: "Mixed agent",
      scopes: ["work"],
      capabilities: ["read"],
      state: "active",
      created_at: "2026-09-10T00:00:00Z",
      last_used_at: "2026-09-19T10:00:00Z",
    },
  ],
};

// Covers Fix 2: fetch/related's result_count 0 always means refused-or-not-found (04 section
// 14 deliberately refuses an unknown slug and an out-of-scope slug identically), while
// search's result_count 0 is a legitimate, honest empty result — these must render two
// different notes, never the same one.
const ACTIVITY_MIXED: AgentActivityResponse = {
  agent_name: "Mixed agent",
  activity: [
    {
      id: 4,
      when: "2026-09-19T10:00:00Z",
      tool: "fetch",
      scopes: ["work"],
      query: "alerting-quiet-rule-needs-positive-control",
      result_count: 0,
      latency_ms: 8,
    },
    {
      id: 3,
      when: "2026-09-19T09:00:00Z",
      tool: "fetch",
      scopes: ["work"],
      query: "ssh-hardening",
      result_count: 1,
      latency_ms: 9,
    },
    {
      id: 2,
      when: "2026-09-19T08:00:00Z",
      tool: "related",
      scopes: ["work"],
      query: "alerting-quiet-rule-needs-positive-control",
      result_count: 0,
      latency_ms: 11,
    },
    {
      id: 1,
      when: "2026-09-19T07:00:00Z",
      tool: "search",
      scopes: ["work"],
      query: "a query that genuinely matches nothing",
      result_count: 0,
      latency_ms: 14,
    },
  ],
};

const ACTIVITY_ACTIVE: AgentActivityResponse = {
  agent_name: "Claude Code",
  activity: [
    {
      id: 2,
      when: "2026-09-19T09:40:00Z",
      tool: "remember",
      scopes: ["work"],
      query: "apply during a window, then re-run the connection stage",
      result_count: 0,
      latency_ms: 12,
    },
    {
      id: 1,
      when: "2026-09-19T08:00:00Z",
      tool: "search",
      scopes: ["work"],
      query: "how is SSH reaching the internet",
      result_count: 3,
      latency_ms: 45,
    },
  ],
};

const EMPTY_ACTIVITY: AgentActivityResponse = { agent_name: "Old bot", activity: [] };

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body };
}

function mockFetch() {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/agents/a-active/activity")) return jsonResponse(ACTIVITY_ACTIVE);
    if (url.includes("/api/agents/a-revoked/activity")) return jsonResponse(EMPTY_ACTIVITY);
    if (url.includes("/api/agents/a-expired/activity")) {
      return jsonResponse({ agent_name: "Temp helper", activity: [] });
    }
    if (url.includes("/api/agents/a-mixed/activity")) return jsonResponse(ACTIVITY_MIXED);
    if (url.endsWith("/api/agents")) return jsonResponse(AGENTS);
    throw new Error(`unexpected fetch: ${url}`);
  });
}

// A minimal, but shape-correct, GET /api/settings body — only its `scopes` field is ever read
// by the Add form (see AgentsScreen's own doc comment on why it, not the allScopes prop, is
// the source for the checkbox list). "personal" has zero pages and zero agents on purpose: a
// space with nothing in it yet must still be a selectable checkbox.
const SETTINGS_WITH_SPACES = {
  types: [],
  tag_groups: [],
  tags: [],
  relations: [],
  scopes: [
    { name: "work", folder: "work", is_client: false, count: 3, agents: ["Claude Code"] },
    { name: "client-globex", folder: "client-globex", is_client: true, count: 2, agents: [] },
    { name: "personal", folder: "personal", is_client: false, count: 0, agents: [] },
  ],
  sources: {
    vault_path: "/tmp/vault", is_git_repo: true, db_host: "localhost", db_name: "balise",
    page_count: 5, commit_count: 5,
  },
  models: { gateway_url: "", gateway_url_set: false, api_key_set: false, note: "" },
};

// Method-aware mock covering everything the Add/Revoke flows touch: GET /api/agents,
// GET /api/agents/{id}/activity, GET /api/settings (fetched lazily, only once the Add form
// opens), POST /api/agents (create), and POST /api/agents/{id}/revoke.
function mockWriteFetch() {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (method === "GET" && url.includes("/api/agents/a-active/activity")) {
      return jsonResponse(ACTIVITY_ACTIVE);
    }
    if (method === "GET" && url.endsWith("/api/agents")) return jsonResponse(AGENTS);
    if (method === "GET" && url.endsWith("/api/settings")) return jsonResponse(SETTINGS_WITH_SPACES);
    if (method === "POST" && url.endsWith("/api/agents")) {
      const body = JSON.parse(String(init?.body ?? "{}"));
      if (!body.name) return jsonResponse({ detail: "agent name is required" }, 400);
      if (!Array.isArray(body.scopes) || body.scopes.length === 0) {
        return jsonResponse({ detail: "at least one space is required" }, 400);
      }
      return jsonResponse(
        {
          agent: {
            id: "a-new",
            name: body.name,
            scopes: body.scopes,
            capabilities: body.capabilities ?? [],
            state: "active",
            created_at: "2026-09-21T00:00:00Z",
            last_used_at: null,
          },
          token: "raw-token-value-shown-once",
        },
        201,
      );
    }
    if (method === "POST" && url.endsWith("/api/agents/a-active/revoke")) {
      return jsonResponse({
        agent: {
          id: "a-active",
          name: "Claude Code",
          scopes: ["work", "client-globex"],
          capabilities: ["read", "remember"],
          state: "revoked",
          created_at: "2026-09-01T00:00:00Z",
          last_used_at: "2026-09-19T09:40:00Z",
        },
      });
    }
    throw new Error(`unexpected fetch: ${method} ${url}`);
  });
}

beforeEach(() => vi.restoreAllMocks());

describe("AgentsScreen", () => {
  it("shows each agent's scopes and capability plainly, never a token, hash or budget", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<AgentsScreen allScopes={["work", "client-globex", "personal"]} />);
    // "Claude Code" renders twice once loaded (list row + detail heading); the heading role
    // pins this wait to the one that only exists after the detail card has data.
    expect(await screen.findByRole("heading", { name: "Claude Code" })).toBeTruthy();
    expect(screen.getByText(/reads work and client-globex/i)).toBeTruthy();
    expect(screen.getByText(/may read and remember/i)).toBeTruthy();

    const html = document.body.innerHTML;
    expect(html.toLowerCase()).not.toContain("hash");
    expect(html.toLowerCase()).not.toContain("secret");
    expect(html.toLowerCase()).not.toContain("budget");
    expect(html).not.toMatch(/\buid\b/);
  });

  it("states the access boundary in one sentence for the selected (active) agent", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<AgentsScreen allScopes={["work", "client-globex", "personal"]} />);
    expect(
      await screen.findByText("This agent can read work and client-globex. It cannot read personal."),
    ).toBeTruthy();
  });

  it("renders revoked and expired agents as visibly distinct from active ones", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    expect(screen.getByText("revoked")).toBeTruthy();
    expect(screen.getByText("expired")).toBeTruthy();
    // "active" renders twice by default (its list row plus the detail head, since the first
    // agent is selected initially) — assert presence via getAllByText rather than the
    // single-match getByText.
    expect(screen.getAllByText("active").length).toBeGreaterThan(0);
  });

  it("shows a revoked agent's boundary sentence and activity honestly", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    await user.click(screen.getByRole("button", { name: /old bot/i }));
    expect(await screen.findByText(/this agent's access was revoked/i)).toBeTruthy();
    expect(await screen.findByText(/has not looked up anything yet/i)).toBeTruthy();
  });

  it("builds the activity list from when/tool/query, with pages opened only for a search", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<AgentsScreen />);
    expect(await screen.findByText(/looked up: how is ssh reaching the internet/i)).toBeTruthy();
    expect(screen.getByText(/opened 3 pages/i)).toBeTruthy();
    expect(
      screen.getByText(/remembered: apply during a window, then re-run the connection stage/i),
    ).toBeTruthy();
    // A "remember" row does not get a fabricated "opened 0 pages" note.
    expect(screen.queryByText(/opened 0 pages/i)).toBeNull();
  });

  it("labels a refused fetch/related (result_count 0) as not found, never as an error", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    await user.click(screen.getByRole("button", { name: /mixed agent/i }));

    const notes = await screen.findAllByText(
      /no matching page — not found, or outside what this agent can read\./i,
    );
    // Exactly the two zero-result fetch/related rows get the note — not the successful
    // fetch, and not the genuinely-empty search below.
    expect(notes).toHaveLength(2);
    expect(document.body.innerHTML.toLowerCase()).not.toContain("denied");
  });

  it("does not label a successful fetch (result_count >= 1) as not found", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    await user.click(screen.getByRole("button", { name: /mixed agent/i }));

    expect(await screen.findByText(/fetch: ssh-hardening/i)).toBeTruthy();
    // Only the two zero-result rows (asserted above) get the not-found note; the
    // successful fetch row must render no note of its own.
    expect(
      await screen.findAllByText(
        /no matching page — not found, or outside what this agent can read\./i,
      ),
    ).toHaveLength(2);
  });

  it("still shows a genuinely empty search as an ordinary opened-0-pages note", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    await user.click(screen.getByRole("button", { name: /mixed agent/i }));

    expect(
      await screen.findByText(/looked up: a query that genuinely matches nothing/i),
    ).toBeTruthy();
    expect(screen.getByText(/opened 0 pages/i)).toBeTruthy();
  });

  it("ships Connect and Edit as honestly-labelled disabled stubs", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    const connect = screen.getByRole("tab", { name: "Connect" });
    const edit = screen.getByRole("tab", { name: "Edit" });
    expect(connect.hasAttribute("disabled")).toBe(true);
    expect(edit.hasAttribute("disabled")).toBe(true);
    expect(connect.getAttribute("title")).toMatch(/shown once, when an agent is created/i);
    expect(edit.getAttribute("title")).toMatch(/cannot be changed in place/i);
  });

  it("shows an honest empty state, and a working Add agent affordance, when no agents exist", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({ agents: [] })));
    render(<AgentsScreen />);
    expect(await screen.findByText(/no agents have been given access/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add agent" })).toBeTruthy();
  });

  it("shows a server-unreachable empty state on fetch failure", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({ detail: "boom" }, 500)));
    render(<AgentsScreen />);
    expect(await screen.findByText(/could not reach the server/i)).toBeTruthy();
  });

  it("does not fetch /api/settings until the Add form is opened, then lists real spaces", async () => {
    const fetchMock = mockWriteFetch();
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });

    expect(fetchMock.mock.calls.some(([i]) => String(i).endsWith("/api/settings"))).toBe(false);

    await user.click(screen.getByRole("button", { name: "Add agent" }));
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([i]) => String(i).endsWith("/api/settings"))).toBe(true);
    });
    // "personal" has zero pages and would never appear via the allScopes/tree prop — proof
    // the checkbox list is really sourced from /api/settings, not allScopes.
    expect(await screen.findByRole("checkbox", { name: "personal" })).toBeTruthy();
  });

  it("requires a name and at least one space before submitting the Add form", async () => {
    vi.stubGlobal("fetch", mockWriteFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    await user.click(screen.getByRole("button", { name: "Add agent" }));
    await screen.findByRole("checkbox", { name: "work" });

    await user.click(screen.getByRole("button", { name: "Add agent" }));
    expect(await screen.findByText(/name the agent/i)).toBeTruthy();

    await user.type(screen.getByLabelText("Name"), "New Agent");
    await user.click(screen.getByRole("button", { name: "Add agent" }));
    expect(await screen.findByText(/choose at least one space/i)).toBeTruthy();
  });

  it("creates an agent, reveals its one-time access key, and supports copying it", async () => {
    // jsdom ships its own real navigator.clipboard (an EventTarget-backed Clipboard
    // instance) rather than leaving the property absent or a plain getter — replacing
    // the whole object (via Object.defineProperty or Object.assign) either throws or is
    // silently ignored on read, because Navigator's WebIDL wrapper serves `.clipboard`
    // through its own accessor regardless of an own-property override. Spying on the
    // existing instance's writeText method is what actually intercepts the call.
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    vi.stubGlobal("fetch", mockWriteFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });
    await user.click(screen.getByRole("button", { name: "Add agent" }));
    await screen.findByRole("checkbox", { name: "work" });

    await user.type(screen.getByLabelText("Name"), "New Agent");
    await user.click(screen.getByRole("checkbox", { name: "work" }));
    await user.click(screen.getByRole("button", { name: "Add agent" }));

    expect(await screen.findByText(/only time new agent.?s access key is shown/i)).toBeTruthy();
    expect(screen.getByText("raw-token-value-shown-once")).toBeTruthy();
    // The form closes and the new agent is now in the list — no second GET /api/agents
    // round trip needed to see it.
    expect(await screen.findByRole("heading", { name: "New Agent" })).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(await screen.findByRole("button", { name: "Copied" })).toBeTruthy();
    expect(writeText).toHaveBeenCalledWith("raw-token-value-shown-once");

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("raw-token-value-shown-once")).toBeNull();
  });

  it("requires confirmation before revoking, then stops offering the agent as revocable", async () => {
    vi.stubGlobal("fetch", mockWriteFetch());
    const user = userEvent.setup();
    render(<AgentsScreen />);
    await screen.findByRole("heading", { name: "Claude Code" });

    await user.click(screen.getByRole("button", { name: "Revoke" }));
    expect(await screen.findByText(/revoke claude code\?/i)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByText(/revoke claude code\?/i)).toBeNull();

    await user.click(screen.getByRole("button", { name: "Revoke" }));
    await user.click(screen.getByRole("button", { name: "Yes, revoke it" }));

    expect(await screen.findByText(/this agent's access was revoked/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Revoke" })).toBeNull();
  });
});
