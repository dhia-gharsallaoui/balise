import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { PageReader } from "../../src/components/knowledge/PageDetail";
import type { PageDetail } from "../../src/lib/types";

// `space` added to satisfy the real PageDetail/PageRow shape (see
// pagerow.test.tsx note — task-6-brief's fixture predates types.ts's wiring
// to the live wire format).
const PAGE: PageDetail = {
  slug: "ergw",
  uid: "u1",
  title: "AzAPI ER gateway update DELETES its connections",
  type: "gotcha",
  status: "active",
  scope: "work",
  space: "Azure",
  historical: false,
  claims_count: 2,
  tokens: 400,
  owner: "dhia",
  last_verified: "2026-08-30",
  tags: ["vendor/azure/expressroute"],
  claims: [
    { text: "Updating the gateway removes every connection", status: "active", as_of: null },
    { text: "Old workaround was a manual re-create", status: "superseded", as_of: "2026-06-10" },
  ],
  body_md: "Any PUT replaces the resource.\n\n## What to do\n\n- Apply during a window.",
  relations: { specializes: [{ slug: "expressroute", title: "ExpressRoute", status: "active" }] },
  backlinks: [{ slug: "vendor-azure-expressroute", title: "ExpressRoute vendor notes", status: "active" }],
  sources: ["Incident notes, 12 Jun 2026"],
  lint: [{ rule: "oversize", severity: "warn", detail: "4,100 tokens > max 1,200" }],
};

describe("PageReader", () => {
  it("shows a loading state while the page is still being fetched", () => {
    // Knowledge.tsx only mounts PageReader once a page is open, so `page === null` here
    // means "fetch in flight", not "nothing selected" — there is no longer a reader column
    // that sits empty until something is opened.
    render(<PageReader page={null} onBack={vi.fn()} />);
    expect(screen.getByText(/Fetching claims, body, and relations/)).toBeTruthy();
  });

  it("gives a way back while still loading", () => {
    render(<PageReader page={null} onBack={vi.fn()} />);
    expect(screen.getByRole("button", { name: /back to knowledge/i })).toBeTruthy();
  });

  it("renders the title and type", () => {
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByRole("heading", { name: PAGE.title })).toBeTruthy();
    expect(screen.getByText("gotcha")).toBeTruthy();
  });

  it("puts the claims ledger before the body", () => {
    const { container } = render(<PageReader page={PAGE} onBack={vi.fn()} />);
    const html = container.innerHTML;
    expect(html.indexOf("removes every connection")).toBeLessThan(html.indexOf("PUT replaces"));
  });

  it("dates a superseded claim in the ledger", () => {
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByText(/superseded Jun 2026/)).toBeTruthy();
  });

  it("renders body markdown as blocks", () => {
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByRole("heading", { name: "What to do" })).toBeTruthy();
    expect(screen.getByText("Apply during a window.")).toBeTruthy();
  });

  it("lists owner, verified date, scope and tags", () => {
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByText("dhia")).toBeTruthy();
    expect(screen.getByText("work")).toBeTruthy();
    expect(screen.getByText("vendor/azure/expressroute")).toBeTruthy();
  });

  it("groups related pages under their role", () => {
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByText(/Instance of/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: "ExpressRoute" })).toBeTruthy();
  });

  it("lists backlinks", () => {
    // page.backlinks was already on the wire (PageDetail type) but rendered nowhere until
    // the reading-view redesign — see reading-view-report.md.
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByText(/Backlinks/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: "ExpressRoute vendor notes" })).toBeTruthy();
  });

  it("surfaces a lint warning", () => {
    render(<PageReader page={PAGE} onBack={vi.fn()} />);
    expect(screen.getByRole("alert").textContent).toContain("4,100 tokens");
  });

  it("goes back on the back control", async () => {
    const onBack = vi.fn();
    render(<PageReader page={PAGE} onBack={onBack} />);
    await userEvent.click(screen.getByRole("button", { name: /back to knowledge/i }));
    expect(onBack).toHaveBeenCalled();
  });
});
