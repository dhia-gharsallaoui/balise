import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SourcesScreen } from "../../src/components/sources/SourcesScreen";
import type { SourcesLogResponse, SourcesUploadResult } from "../../src/lib/types";

// SourcesScreen owns its own /api/sources/log fetching and posts to /api/sources/pages
// directly (lib/api.ts's uploadSourcePage cannot use the JSON-only `post` helper — the body
// is raw text, not a JSON envelope) — so these tests stub the global fetch, the same
// convention agentsscreen.test.tsx and reviewscreen.test.tsx use.

const LOG: SourcesLogResponse = {
  ingests: [
    {
      sha: "abc123",
      path: "work/notes/ssh-hardening.md",
      message: "sources: add work/ssh-hardening",
      author_word: "you",
      when: "2026-09-19T10:00:00Z",
      scope: "work",
      slug: "ssh-hardening",
      title: "SSH hardening",
    },
    {
      // No scope/slug/title: a real tracked commit the indexer never turned into a page
      // (e.g. a memory import). Must render as plain, inert text, never a broken link.
      sha: "def456",
      path: "memory/2026-09-18.md",
      message: "memory: import 2026-09-18",
      author_word: "an agent",
      when: "2026-09-18T08:00:00Z",
    },
  ],
};

const UPLOAD_SUCCESS: SourcesUploadResult = {
  commit: "abc999",
  scope: "work",
  slug: "pasted-note",
  path: "work/notes/pasted-note.md",
  title: "Pasted Note",
  index_stale: false,
};

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body };
}

function mockFetch(overrides: { uploadStatus?: number; uploadBody?: unknown } = {}) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.includes("/api/sources/log")) return jsonResponse(LOG);
    if (url.includes("/api/sources/pages") && init?.method === "POST") {
      return jsonResponse(overrides.uploadBody ?? UPLOAD_SUCCESS, overrides.uploadStatus ?? 201);
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
}

beforeEach(() => vi.restoreAllMocks());

describe("SourcesScreen", () => {
  it("renders an honest empty state for Connected sources, no fabricated connector rows", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<SourcesScreen allScopes={["work", "client-globex"]} />);
    expect(await screen.findByText(/no sources are connected/i)).toBeTruthy();
    expect(screen.getByText(/dropping a file or pasting text/i)).toBeTruthy();
  });

  it("renders the ingest log from real git history, clickable only when a page resolved", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<SourcesScreen allScopes={["work"]} />);
    expect(await screen.findByRole("button", { name: /open ssh hardening/i })).toBeTruthy();
    // The unresolved memory-import row renders as plain static text, not a button.
    expect(screen.getByText(/memory: import 2026-09-18/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /memory: import/i })).toBeNull();
  });

  it("opens a resolved ingest row through onOpenPage", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const onOpenPage = vi.fn();
    const user = userEvent.setup();
    render(<SourcesScreen allScopes={["work"]} onOpenPage={onOpenPage} />);
    await user.click(await screen.findByRole("button", { name: /open ssh hardening/i }));
    expect(onOpenPage).toHaveBeenCalledWith(
      [{ scope: "work", slug: "ssh-hardening", title: "SSH hardening" }],
      expect.stringContaining("SSH hardening"),
    );
  });

  it("shows an honest empty state when the ingest log has no entries", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/sources/log")) return jsonResponse({ ingests: [] });
        throw new Error(`unexpected fetch: ${url}`);
      }),
    );
    render(<SourcesScreen allScopes={["work"]} />);
    expect(await screen.findByText(/nothing has been added yet/i)).toBeTruthy();
  });

  it("submits pasted text and shows success feedback with an Open it action", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const onOpenPage = vi.fn();
    const user = userEvent.setup();
    render(<SourcesScreen allScopes={["work", "client-globex"]} onOpenPage={onOpenPage} />);
    await screen.findByText(/no sources are connected/i);

    // Exact string (not a regex) so this can't also match the drop-zone heading "Drop files
    // here or paste text", which contains the same "or paste text" substring.
    await user.type(screen.getByLabelText("Or paste text"), "# Pasted Note\n\nSome content.");
    await user.click(screen.getByRole("button", { name: /add to vault/i }));

    expect(await screen.findByText(/added "pasted note" to work/i)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: /open it/i }));
    expect(onOpenPage).toHaveBeenCalledWith(
      [{ scope: "work", slug: "pasted-note", title: "Pasted Note" }],
      expect.stringContaining("Pasted Note"),
    );
  });

  it("loads a selected markdown file into the form and shows its name", async () => {
    vi.stubGlobal("fetch", mockFetch());
    const user = userEvent.setup();
    render(<SourcesScreen allScopes={["work"]} />);
    await screen.findByText(/no sources are connected/i);

    const file = new File(["# From File\n\nBody text."], "notes.md", { type: "text/markdown" });
    await user.upload(screen.getByLabelText(/choose a file/i), file);

    expect(await screen.findByText(/loaded "notes\.md"/i)).toBeTruthy();
  });

  it("refuses a file that is neither markdown nor plain text", async () => {
    vi.stubGlobal("fetch", mockFetch());
    // applyAccept: false — the real browser's `accept` attribute on the file input only
    // filters the OS picker dialog, it never blocks drag-and-drop or a deliberately chosen
    // "All Files" selection, so the component's own type check is the real guard. Disabling
    // user-event's accept emulation here lets this test exercise that guard directly, instead
    // of user-event silently dropping the mismatched file before it reaches the component.
    const user = userEvent.setup({ applyAccept: false });
    render(<SourcesScreen allScopes={["work"]} />);
    await screen.findByText(/no sources are connected/i);

    const file = new File(["not really text"], "photo.png", { type: "image/png" });
    await user.upload(screen.getByLabelText(/choose a file/i), file);

    expect(
      await screen.findByText(/only markdown and plain text files are supported/i),
    ).toBeTruthy();
  });

  it("shows a friendly retry message on a 409 slug collision", async () => {
    vi.stubGlobal(
      "fetch",
      mockFetch({ uploadStatus: 409, uploadBody: { detail: "a page already exists at this path" } }),
    );
    const user = userEvent.setup();
    render(<SourcesScreen allScopes={["work"]} />);
    await screen.findByText(/no sources are connected/i);
    await user.type(screen.getByLabelText("Or paste text"), "Some pasted content.");
    await user.click(screen.getByRole("button", { name: /add to vault/i }));
    expect(
      await screen.findByText(/already exists in work\. try a different title\./i),
    ).toBeTruthy();
  });

  it("shows a friendly size-limit message on a 413 response", async () => {
    vi.stubGlobal(
      "fetch",
      mockFetch({ uploadStatus: 413, uploadBody: { detail: "content too large" } }),
    );
    const user = userEvent.setup();
    render(<SourcesScreen allScopes={["work"]} />);
    await screen.findByText(/no sources are connected/i);
    await user.type(screen.getByLabelText("Or paste text"), "Some pasted content.");
    await user.click(screen.getByRole("button", { name: /add to vault/i }));
    expect(await screen.findByText(/2 mb limit/i)).toBeTruthy();
  });

  it("never says uid, index, token or budget on screen", async () => {
    vi.stubGlobal("fetch", mockFetch());
    render(<SourcesScreen allScopes={["work"]} />);
    await screen.findByText(/no sources are connected/i);
    const html = document.body.innerHTML.toLowerCase();
    expect(html).not.toContain("budget");
    expect(html).not.toMatch(/\buid\b/);
    expect(html).not.toMatch(/\btoken\b/);
    expect(html).not.toMatch(/\bindex\b/);
  });

  it("shows a server-unreachable-style empty state honestly when the log fails to load", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/sources/log")) return jsonResponse({ detail: "boom" }, 500);
        throw new Error(`unexpected fetch: ${url}`);
      }),
    );
    render(<SourcesScreen allScopes={["work"]} />);
    expect(await screen.findByText(/could not load the ingest log/i)).toBeTruthy();
  });
});
