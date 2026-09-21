import { describe, expect, it } from "vitest";
import { plural, pluralise } from "../../src/lib/plural";

describe("plural", () => {
  it("leaves a count of exactly one singular", () => {
    expect(plural(1, "page")).toBe("1 page");
    expect(plural(1, "entry")).toBe("1 entry");
  });

  it("pluralises zero, as English does", () => {
    expect(plural(0, "page")).toBe("0 pages");
    expect(plural(0, "entry")).toBe("0 entries");
  });

  it("turns consonant-y into -ies, which is the bug this file exists for", () => {
    expect(plural(3, "entry")).toBe("3 entries");
    expect(plural(2, "property")).toBe("2 properties");
  });

  it("leaves vowel-y alone", () => {
    expect(plural(3, "day")).toBe("3 days");
    expect(plural(2, "key")).toBe("2 keys");
  });

  it("adds -es after a sibilant", () => {
    expect(plural(2, "match")).toBe("2 matches");
    expect(plural(2, "index")).toBe("2 indexes");
    expect(plural(2, "class")).toBe("2 classes");
  });

  it("keeps every noun the screens actually pass regular", () => {
    for (const [unit, expected] of [
      ["page", "pages"], ["commit", "commits"], ["edge", "edges"],
      ["proposal", "proposals"], ["item", "items"], ["value", "values"],
      ["hour", "hours"], ["minute", "minutes"], ["type", "types"],
    ] as const) {
      expect(plural(2, unit)).toBe(`2 ${expected}`);
    }
  });

  it("exposes the bare plural for callers that render the count separately", () => {
    expect(pluralise("entry")).toBe("entries");
    expect(pluralise("page")).toBe("pages");
  });
});
