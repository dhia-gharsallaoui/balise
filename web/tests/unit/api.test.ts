import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, fetchPage, fetchPages, search } from "../../src/lib/api";

function mockFetch(body: unknown, status = 200) {
  return vi.fn().mockResolvedValue({
    ok: status < 400, status,
    json: async () => body,
  });
}

beforeEach(() => vi.restoreAllMocks());

describe("api client", () => {
  it("requests the pages endpoint and returns groups", async () => {
    const fetcher = mockFetch({ groups: [{ type: "state", count: 0, pages: [] }], total: 0 });
    vi.stubGlobal("fetch", fetcher);
    const result = await fetchPages({});
    expect(result.groups[0].type).toBe("state");
    expect(fetcher.mock.calls[0][0]).toContain("/api/pages");
  });

  it("serialises filters into the query string", async () => {
    const fetcher = mockFetch({ groups: [], total: 0 });
    vi.stubGlobal("fetch", fetcher);
    await fetchPages({ type: "gotcha", historical: true, tags: ["vendor/azure"] });
    const url = fetcher.mock.calls[0][0] as string;
    expect(url).toContain("type=gotcha");
    expect(url).toContain("historical=true");
    expect(url).toContain("tags=vendor%2Fazure");
  });

  it("omits filters that are not set", async () => {
    const fetcher = mockFetch({ groups: [], total: 0 });
    vi.stubGlobal("fetch", fetcher);
    await fetchPages({});
    expect(fetcher.mock.calls[0][0]).not.toContain("type=");
  });

  it("throws ApiError with the status on a failure", async () => {
    vi.stubGlobal("fetch", mockFetch({ detail: "not found" }, 404));
    const missing = { scope: "work", slug: "nope" };
    await expect(fetchPage(missing)).rejects.toThrow(ApiError);
    await expect(fetchPage(missing)).rejects.toMatchObject({ status: 404 });
  });

  // A page is identified by (scope, slug). The route used to be /api/pages/{slug}, which
  // served whichever scope sorted first when two scopes held the same slug.
  it("addresses a page by scope and slug", async () => {
    const fetcher = mockFetch({});
    vi.stubGlobal("fetch", fetcher);
    await fetchPage({ scope: "client-globex", slug: "expressroute" });
    expect(fetcher.mock.calls[0][0]).toContain("/api/pages/client-globex/expressroute");
  });

  it("escapes a scope and slug that need it", async () => {
    const fetcher = mockFetch({});
    vi.stubGlobal("fetch", fetcher);
    await fetchPage({ scope: "client globex", slug: "a/b" });
    expect(fetcher.mock.calls[0][0]).toContain("/api/pages/client%20globex/a%2Fb");
  });

  it("returns low coverage through unchanged", async () => {
    vi.stubGlobal("fetch", mockFetch({ hits: [], coverage: "low" }));
    expect((await search("nothing")).coverage).toBe("low");
  });
});
