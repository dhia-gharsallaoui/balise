import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ReviewScreen } from "../../src/components/review/ReviewScreen";
import type { ReviewDetail, ReviewListResponse } from "../../src/lib/types";

// ReviewScreen owns its own /api/review fetching (list + per-item detail + accept/reject),
// the same self-contained-data-fetching pattern Knowledge.tsx already uses — so these tests
// stub the global fetch (api.test.ts's convention) rather than mocking the api module.

const LIST: ReviewListResponse = {
  proposals: [
    {
      id: "p-1",
      kind: "claims",
      scope: "client-globex",
      target: "client-globex/gotchas/alloy.md",
      target_title: "Alloy duplicate component kills telemetry",
      confidence: "likely",
      created_by: "compile:2026-09-18T14:25:08Z",
    },
    {
      id: "p-2",
      kind: "claims",
      scope: "client-globex",
      target: "client-globex/gotchas/arc.md",
      target_title: "Arc onboarding gotchas",
      confidence: "possible",
      created_by: "compile:2026-09-17T10:00:00Z",
    },
  ],
};

const DETAIL_1: ReviewDetail = {
  id: "p-1",
  kind: "claims",
  scope: "client-globex",
  target: "client-globex/gotchas/alloy.md",
  target_title: "Alloy duplicate component kills telemetry",
  confidence: "likely",
  created_by: "compile:2026-09-18T14:25:08Z",
  evidence: [{ source: "client-globex/gotchas/alloy.md", text: "Duplicate prometheus.scrape label kills telemetry." }],
  before: [{ id: "c1", text: "Duplicate component label kills telemetry", status: "active", mark: "unchanged" }],
  after: [
    { id: "c1", text: "Duplicate prometheus.scrape label kills telemetry", status: "active", mark: "reworded" },
    { id: "c2", text: "Fixed by roles/node_monitoring owning /etc/alloy", status: "active", mark: "added" },
  ],
};

const DETAIL_2: ReviewDetail = {
  ...DETAIL_1,
  id: "p-2",
  target_title: "Arc onboarding gotchas",
  evidence: [{ source: "client-globex/gotchas/arc.md", text: "A reimage leaves a live ARM Arc record." }],
};

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body };
}

function mockFetch() {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    if (method === "GET" && url.includes("/api/review/p-1")) return jsonResponse(DETAIL_1);
    if (method === "GET" && url.includes("/api/review/p-2")) return jsonResponse(DETAIL_2);
    if (method === "GET" && url.endsWith("/api/review")) return jsonResponse(LIST);
    if (method === "POST" && url.includes("/accept")) {
      return jsonResponse({ commit: "deadbeef", index_stale: false, before: DETAIL_1.before, after: DETAIL_1.after });
    }
    if (method === "POST" && url.includes("/reject")) {
      return jsonResponse({ commit: "cafefeed", path: "review/.rejected/p-2.md" });
    }
    if (method === "POST" && url.includes("/edit-accept")) {
      return jsonResponse({
        commit: "edabeef",
        index_stale: false,
        before: DETAIL_1.before,
        after: DETAIL_1.after,
        warnings: [],
      });
    }
    throw new Error(`unexpected fetch: ${method} ${url}`);
  });
}

beforeEach(() => vi.restoreAllMocks());

describe("ReviewScreen", () => {
  it("shows confidence as a word in the queue, never a percentage", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    expect(await screen.findByText("likely")).toBeTruthy();
    expect(screen.getByText("possible")).toBeTruthy();
    expect(screen.queryByText(/%/)).toBeNull();
  });

  it("shows evidence first, then before/after claims marked added/reworded", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    expect(await screen.findByText(/Duplicate prometheus.scrape label kills telemetry\./)).toBeTruthy();
    const html = document.body.innerHTML;
    expect(html.indexOf("Evidence")).toBeLessThan(html.indexOf("Current"));
    expect(screen.getByText("reworded")).toBeTruthy();
    expect(screen.getByText("added")).toBeTruthy();
  });

  it("opens a real editing panel for each added/reworded claim, not a disabled stub", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    const editButton = screen.getByRole("button", { name: /^edit then accept$/i });
    expect(editButton.hasAttribute("disabled")).toBe(false);
    await user.click(editButton);
    // p-1's after list has one reworded (c1) and one added (c2) claim — both editable.
    expect(await screen.findByLabelText(/reworded claim/i)).toBeTruthy();
    expect(screen.getByLabelText(/new claim/i)).toBeTruthy();
  });

  it("edits a claim's text, accepts, and posts only the changed claim", async () => {
    const fetcher = mockFetch();
    vi.stubGlobal("fetch", fetcher);
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    await user.click(screen.getByRole("button", { name: /^edit then accept$/i }));
    const newClaimField = await screen.findByLabelText(/new claim/i);
    await user.clear(newClaimField);
    await user.type(newClaimField, "Fixed by roles/node_monitoring owning the whole directory");
    await user.click(screen.getByRole("button", { name: /confirm edit and accept/i }));
    await waitFor(() => {
      const call = fetcher.mock.calls.find(
        ([u, i]) => String(u).includes("/api/review/p-1/edit-accept") && (i as RequestInit)?.method === "POST",
      );
      expect(call).toBeTruthy();
      const body = JSON.parse(String((call?.[1] as RequestInit)?.body));
      expect(body.edits).toEqual([{ id: "c2", text: "Fixed by roles/node_monitoring owning the whole directory" }]);
    });
  });

  it("warns about a too-long edited claim as text, not only via colour, but still allows accept", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    await user.click(screen.getByRole("button", { name: /^edit then accept$/i }));
    const newClaimField = await screen.findByLabelText(/new claim/i);
    await user.clear(newClaimField);
    await user.type(
      newClaimField,
      "This claim has been reworded to contain far more than the usual fifteen word limit for a single claim",
    );
    expect(await screen.findByText(/longer than the usual limit of 15, but this will still be accepted/i)).toBeTruthy();
    const confirmButton = screen.getByRole("button", { name: /confirm edit and accept/i });
    expect(confirmButton.hasAttribute("disabled")).toBe(false);
  });

  it("blocks accept when an edited claim is too short, and explains why as text", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    await user.click(screen.getByRole("button", { name: /^edit then accept$/i }));
    const newClaimField = await screen.findByLabelText(/new claim/i);
    await user.clear(newClaimField);
    await user.type(newClaimField, "onewordstop");
    expect(await screen.findByText(/too short to read as a statement/i)).toBeTruthy();
    const confirmButton = screen.getByRole("button", { name: /confirm edit and accept/i });
    expect(confirmButton.hasAttribute("disabled")).toBe(true);
  });

  it("wires the 'e' keyboard shortcut to open the editing panel", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    await user.keyboard("e");
    expect(await screen.findByLabelText(/new claim/i)).toBeTruthy();
  });

  it("accepts a proposal and advances to the next item", async () => {
    const fetcher = mockFetch();
    vi.stubGlobal("fetch", fetcher);
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    await user.click(screen.getByRole("button", { name: "Accept" }));
    await waitFor(() => {
      expect(fetcher.mock.calls.some(([u, i]) => String(u).includes("/api/review/p-1/accept") && (i as RequestInit)?.method === "POST")).toBe(true);
    });
  });

  it("requires a reason before rejecting, then posts it", async () => {
    const fetcher = mockFetch();
    vi.stubGlobal("fetch", fetcher);
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    await screen.findByText("Alloy duplicate component kills telemetry");
    await user.click(screen.getByRole("button", { name: "Reject" }));
    await user.click(screen.getByRole("button", { name: "Confirm reject" }));
    expect(screen.getByText(/reason must be a single non-empty line/i)).toBeTruthy();

    await user.type(screen.getByLabelText(/reason/i), "Needs a manual trim");
    await user.click(screen.getByRole("button", { name: "Confirm reject" }));
    await waitFor(() => {
      const call = fetcher.mock.calls.find(([u, i]) => String(u).includes("/api/review/p-1/reject") && (i as RequestInit)?.method === "POST");
      expect(call).toBeTruthy();
      const body = JSON.parse(String((call?.[1] as RequestInit)?.body));
      expect(body.reason).toBe("Needs a manual trim");
    });
  });

  it("opens the evidence's page via onOpenPage", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const onOpenPage = vi.fn();
    const user = userEvent.setup();
    render(<ReviewScreen onOpenPage={onOpenPage} />);
    const openButton = await screen.findByRole("button", { name: /open client-globex\/gotchas\/alloy\.md/i });
    await user.click(openButton);
    expect(onOpenPage).toHaveBeenCalledWith("client-globex", "client-globex/gotchas/alloy.md", "Alloy duplicate component kills telemetry");
  });

  it("shows the empty state when nothing is waiting", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({ proposals: [] })));
    render(<ReviewScreen onOpenPage={vi.fn()} />);
    expect(await screen.findByText(/nothing is waiting for review/i)).toBeTruthy();
  });
});
