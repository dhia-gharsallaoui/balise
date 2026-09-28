import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AppShell } from "../../src/components/AppShell";

// Knowledge, Home and Review between them import several api functions; mock all of them or
// rendering the default section throws on an undefined import.
vi.mock("../../src/lib/api", () => ({
  fetchTree: vi.fn().mockResolvedValue({ spaces: [], types: [], historical_count: 0 }),
  fetchPages: vi.fn().mockResolvedValue({ groups: [], total: 0 }),
  fetchPage: vi.fn().mockResolvedValue(null),
  fetchGraph: vi.fn().mockResolvedValue({ nodes: [], edges: [] }),
  search: vi.fn().mockResolvedValue({ hits: [], coverage: "ok" }),
  fetchHome: vi.fn().mockResolvedValue({
    waiting: { total: 0, by_kind: [], proposals: [] },
    attention: [],
    changes: [],
    owner: "dhia",
  }),
  fetchReview: vi.fn().mockResolvedValue({ proposals: [] }),
  fetchReviewItem: vi.fn().mockResolvedValue(null),
  acceptReview: vi.fn(),
  rejectReview: vi.fn(),
  fetchAgents: vi.fn().mockResolvedValue({ agents: [] }),
  fetchAgentActivity: vi.fn().mockResolvedValue({ agent_name: "", activity: [] }),
  fetchSourcesLog: vi.fn().mockResolvedValue({ ingests: [] }),
  uploadSourcePage: vi.fn(),
  fetchSettings: vi.fn().mockResolvedValue({
    types: [],
    tag_groups: [],
    tags: [],
    relations: [],
    scopes: [],
    sources: { vault_path: "", is_git_repo: false, db_host: "", db_name: "", page_count: 0, commit_count: 0 },
    models: { gateway_url: "", gateway_url_set: false, api_key_set: false, note: "" },
  }),
}));

describe("AppShell", () => {
  it("renders all six sections in the rail", () => {
    render(<AppShell />);
    for (const label of ["Home", "Knowledge", "Review", "Sources", "Agents", "Settings"]) {
      expect(screen.getByRole("button", { name: new RegExp(label) })).toBeTruthy();
    }
  });

  it("opens on Knowledge", () => {
    render(<AppShell />);
    expect(screen.getByRole("button", { name: /Knowledge/ }).getAttribute("aria-current")).toBe("page");
  });

  it("renders the Settings screen (not the not-part-of-this-release fallback) when Settings is selected", async () => {
    // All six SECTIONS now have a real screen (see settingsscreen.test.tsx and
    // sourcesscreen.test.tsx), so there is no longer an unbuilt section left for a
    // "not yet built" test to target — this asserts the fallback is unreachable instead.
    render(<AppShell />);
    await userEvent.click(screen.getByRole("button", { name: /^Settings/ }));
    expect(await screen.findByRole("tablist", { name: /settings sections/i })).toBeTruthy();
    expect(screen.queryByText(/not part of this release/i)).toBeNull();
  });

  it("renders the Home briefing when Home is selected", async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole("button", { name: /^Home/ }));
    expect(await screen.findByRole("heading", { name: "Waiting for you" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Needs attention" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Changed recently" })).toBeTruthy();
  });

  it("renders the Review queue when Review is selected", async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole("button", { name: /^Review/ }));
    expect(await screen.findByText(/nothing is waiting for review/i)).toBeTruthy();
  });

  it("renders the Agents screen (not the not-part-of-this-release fallback) when Agents is selected", async () => {
    render(<AppShell />);
    await userEvent.click(screen.getByRole("button", { name: /^Agents/ }));
    expect(await screen.findByText(/no agents have been given access/i)).toBeTruthy();
    expect(screen.queryByText(/not part of this release/i)).toBeNull();
  });

  it("does not render a dead command palette placeholder", () => {
    // The palette was a disabled "coming later" button that did nothing; it was removed
    // rather than left as a control that looks usable and isn't.
    render(<AppShell />);
    expect(screen.queryByRole("button", { name: /Search or jump to/ })).toBeNull();
  });

  it("exposes a theme control with system, light and dark choices", async () => {
    render(<AppShell />);
    const group = screen.getByRole("group", { name: /theme/i });
    const dark = within(group).getByRole("button", { name: /dark/i });
    await userEvent.click(dark);
    expect(dark.getAttribute("aria-pressed")).toBe("true");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    await userEvent.click(within(group).getByRole("button", { name: /system/i }));
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  });

  it("names the current section in the document title", async () => {
    render(<AppShell />);
    // Earlier tests leave the URL on another section, so navigate explicitly first.
    await userEvent.click(screen.getByRole("button", { name: /^Knowledge/ }));
    expect(document.title).toBe("Knowledge · Balise");
    await userEvent.click(screen.getByRole("button", { name: /^Review/ }));
    expect(document.title).toBe("Review · Balise");
  });
});
