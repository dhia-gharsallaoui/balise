import { describe, expect, it } from "vitest";
import { buildUrl, DEFAULT_PARAMS, parseUrl, type UrlParams } from "../../src/lib/urlState";

function params(over: Partial<UrlParams> = {}): UrlParams {
  return { ...DEFAULT_PARAMS, ...over };
}

describe("parseUrl", () => {
  it("reads the section from the path", () => {
    expect(parseUrl("/knowledge", "").section).toBe("knowledge");
    expect(parseUrl("/Review", "").section).toBe("review");
  });

  it("treats the root path as no section, so the app picks its default", () => {
    expect(parseUrl("/", "").section).toBeNull();
    expect(parseUrl("", "").section).toBeNull();
  });

  it("tolerates surrounding slashes", () => {
    expect(parseUrl("//knowledge//", "").section).toBe("knowledge");
  });

  it("reads the search query", () => {
    expect(parseUrl("/knowledge", "?q=alloy").query).toBe("alloy");
  });

  it("round-trips a query needing escaping", () => {
    const url = buildUrl(params({ section: "knowledge", query: "a&b=c d/e" }));
    expect(parseUrl("/knowledge", url.slice(url.indexOf("?"))).query).toBe("a&b=c d/e");
  });

  it("splits a page reference on its first slash only", () => {
    // Slugs cannot contain "/" today, but splitting on the first separator rather than
    // every one keeps this correct if that ever changes.
    expect(parseUrl("/knowledge", "?page=work/a-b-c")).toMatchObject({
      page: { scope: "work", slug: "a-b-c" },
    });
  });

  it("ignores a page reference that is not scope/slug", () => {
    expect(parseUrl("/k", "?page=work").page).toBeNull();
    expect(parseUrl("/k", "?page=/orphan").page).toBeNull();
    expect(parseUrl("/k", "?page=work/").page).toBeNull();
    expect(parseUrl("/k", "?page=").page).toBeNull();
  });

  it("reads hidden types as a comma list, dropping blanks", () => {
    expect(parseUrl("/k", "?hide=State,,Decision, Gotcha ").hiddenTypes)
      .toEqual(["State", "Decision", "Gotcha"]);
  });

  it("only accepts Graph as a non-default view", () => {
    expect(parseUrl("/k", "?view=Graph").view).toBe("Graph");
    expect(parseUrl("/k", "?view=nonsense").view).toBe("List");
    expect(parseUrl("/k", "").view).toBe("List");
  });

  it("reads historical as an explicit 1", () => {
    expect(parseUrl("/k", "?historical=1").historical).toBe(true);
    expect(parseUrl("/k", "?historical=0").historical).toBe(false);
    expect(parseUrl("/k", "").historical).toBe(false);
  });

  it("ignores unknown params rather than failing", () => {
    expect(parseUrl("/knowledge", "?q=x&bogus=1")).toMatchObject({ query: "x" });
  });
});

describe("buildUrl", () => {
  it("omits every default so an untouched view has a clean URL", () => {
    expect(buildUrl(params({ section: "knowledge" }))).toBe("/knowledge");
  });

  it("writes the params that are set", () => {
    const url = buildUrl(params({ section: "knowledge", query: "alloy", space: "work" }));
    expect(url).toBe("/knowledge?q=alloy&space=work");
  });

  it("sorts hidden types so the same filter always produces the same link", () => {
    const a = buildUrl(params({ section: "k", hiddenTypes: ["Gotcha", "State"] }));
    const b = buildUrl(params({ section: "k", hiddenTypes: ["State", "Gotcha"] }));
    expect(a).toBe(b);
    expect(a).toBe("/k?hide=Gotcha,State");
  });

  it("drops a half-formed page reference instead of emitting a broken link", () => {
    expect(buildUrl(params({ section: "k", page: { scope: "work", slug: "" } }))).toBe("/k");
    expect(buildUrl(params({ section: "k", page: { scope: "", slug: "x" } }))).toBe("/k");
  });

  it("renders the default section as the root path", () => {
    expect(buildUrl(params())).toBe("/");
    expect(buildUrl(params({ query: "x" }))).toBe("/?q=x");
  });
});

describe("round trip", () => {
  it("survives every param being set at once", () => {
    const original = params({
      section: "knowledge",
      query: "prom ql",
      space: "client-globex",
      view: "Graph",
      page: { scope: "work", slug: "ai-gateway-routing" },
      hiddenTypes: ["Decision", "State"],
      historical: true,
    });
    const url = buildUrl(original);
    const [path, search] = url.split("?");
    expect(parseUrl(path, search ? `?${search}` : "")).toEqual(original);
  });
});

describe("readable escapes", () => {
  it("leaves commas and slashes unescaped so links stay legible", () => {
    const url = buildUrl(params({
      section: "knowledge",
      page: { scope: "client-globex", slug: "alloy-duplicate-component-kills-telemetry" },
      hiddenTypes: ["Decision", "State"],
    }));
    expect(url).toBe(
      "/knowledge?page=client-globex/alloy-duplicate-component-kills-telemetry&hide=Decision,State",
    );
    expect(url).not.toContain("%2F");
    expect(url).not.toContain("%2C");
  });

  it("still parses the escaped forms, for links written by hand or by another tool", () => {
    const parsed = parseUrl("/knowledge", "?page=client-globex%2Fa-slug&hide=Decision%2CState");
    expect(parsed.page).toEqual({ scope: "client-globex", slug: "a-slug" });
    expect(parsed.hiddenTypes).toEqual(["Decision", "State"]);
  });
});
