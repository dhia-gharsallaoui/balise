import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { PageRow } from "../../src/components/knowledge/PageRow";
import type { PageRow as Row } from "../../src/lib/types";

// PageRow requires `space` (Batch E wired types.ts to the live wire format,
// which the task-5-brief's own fixture predates) — added here so this fixture
// type-checks against the real PageRow shape.
const PAGE: Row = {
  slug: "ergw",
  uid: "u1",
  title: "AzAPI ER gateway update DELETES its connections",
  type: "gotcha",
  status: "active",
  scope: "work",
  space: "work",
  historical: false,
  claims_count: 3,
  tokens: 400,
  owner: "dhia",
  tags: ["vendor/azure/expressroute"],
  claims: [
    { text: "Updating the gateway removes every connection", status: "active", as_of: null },
    { text: "The plan shows nothing to destroy", status: "active", as_of: null },
    { text: "Old workaround was a manual re-create", status: "superseded", as_of: "2026-06-10" },
  ],
};

describe("PageRow", () => {
  it("shows the headline collapsed by default", () => {
    render(<PageRow page={PAGE} onOpen={vi.fn()} />);
    expect(screen.getByText(PAGE.title)).toBeTruthy();
    expect(screen.queryByText(/removes every connection/)).toBeNull();
  });

  it("expands claims when the twist is activated", async () => {
    render(<PageRow page={PAGE} onOpen={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: /expand/i }));
    expect(screen.getByText(/removes every connection/)).toBeTruthy();
  });

  it("collapses again on a second activation", async () => {
    render(<PageRow page={PAGE} onOpen={vi.fn()} />);
    const twist = screen.getByRole("button", { name: /expand/i });
    await userEvent.click(twist);
    await userEvent.click(screen.getByRole("button", { name: /collapse/i }));
    expect(screen.queryByText(/removes every connection/)).toBeNull();
  });

  it("renders every claim status as a word", async () => {
    render(<PageRow page={PAGE} onOpen={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: /expand/i }));
    const superseded = screen.getByText(/Old workaround/).closest("li")!;
    expect(within(superseded).getByText(/superseded/)).toBeTruthy();
  });

  it("dates a historical claim", async () => {
    render(<PageRow page={PAGE} onOpen={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: /expand/i }));
    expect(screen.getByText(/superseded Jun 2026/)).toBeTruthy();
  });

  it("opens the page when the row is activated", async () => {
    const onOpen = vi.fn();
    render(<PageRow page={PAGE} onOpen={onOpen} />);
    await userEvent.click(screen.getByText(PAGE.title));
    expect(onOpen).toHaveBeenCalledWith({ scope: "work", slug: "ergw" });
  });

  it("does not open the page when only the twist is activated", async () => {
    const onOpen = vi.fn();
    render(<PageRow page={PAGE} onOpen={onOpen} />);
    await userEvent.click(screen.getByRole("button", { name: /expand/i }));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("pluralises the claim count for more than one claim", () => {
    render(<PageRow page={PAGE} onOpen={vi.fn()} />);
    expect(screen.getByText(/3 claims/)).toBeTruthy();
  });

  it("does not pluralise the claim count for exactly one claim", () => {
    const onePage: Row = { ...PAGE, claims_count: 1, claims: [PAGE.claims[0]] };
    render(<PageRow page={onePage} onOpen={vi.fn()} />);
    expect(screen.getByText(/1 claim(?!s)/)).toBeTruthy();
    expect(screen.queryByText(/1 claims/)).toBeNull();
  });

  it("marks a historical page", () => {
    render(<PageRow page={{ ...PAGE, historical: true, status: "resolved" }} onOpen={vi.fn()} />);
    expect(screen.getByRole("listitem").dataset.historical).toBe("true");
  });

  it("is reachable by keyboard", async () => {
    const onOpen = vi.fn();
    render(<PageRow page={PAGE} onOpen={onOpen} />);
    await userEvent.tab();
    await userEvent.tab();
    await userEvent.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalled();
  });
});
