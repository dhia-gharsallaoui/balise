import { describe, expect, it } from "vitest";
import { splitTitleSegments, stripBackticks } from "../../src/lib/richTitle";

describe("splitTitleSegments", () => {
  it("returns a single plain-text segment when there are no backticks", () => {
    expect(splitTitleSegments("Arc onboarding gotchas")).toEqual([
      { text: "Arc onboarding gotchas", code: false },
    ]);
  });

  it("splits one pair into text/code/text", () => {
    expect(splitTitleSegments("Alloy config lives at `/etc/alloy`")).toEqual([
      { text: "Alloy config lives at ", code: false },
      { text: "/etc/alloy", code: true },
    ]);
  });

  it("splits several pairs, alternating text and code", () => {
    const title = "AMW lowercases every name in `/api/v1/label/__name__/values`; a `DCGM_FI_` prefix matches zero";
    expect(splitTitleSegments(title)).toEqual([
      { text: "AMW lowercases every name in ", code: false },
      { text: "/api/v1/label/__name__/values", code: true },
      { text: "; a ", code: false },
      { text: "DCGM_FI_", code: true },
      { text: " prefix matches zero", code: false },
    ]);
  });

  it("treats adjacent pairs as back-to-back code segments with no text between", () => {
    expect(splitTitleSegments("`a``b`")).toEqual([
      { text: "a", code: true },
      { text: "b", code: true },
    ]);
  });

  it("renders an unpaired trailing backtick as literal plain text, never dropped", () => {
    expect(splitTitleSegments("nested paths inflate totals ~40%`")).toEqual([
      { text: "nested paths inflate totals ~40%`", code: false },
    ]);
  });

  it("renders an unpaired backtick after a real pair as literal plain text", () => {
    expect(splitTitleSegments("uses `=~` unanchored`")).toEqual([
      { text: "uses ", code: false },
      { text: "=~", code: true },
      { text: " unanchored`", code: false },
    ]);
  });

  it("handles a backtick at both the start and the end, wrapping the whole string", () => {
    expect(splitTitleSegments("`whole title as code`")).toEqual([{ text: "whole title as code", code: true }]);
  });

  it("keeps an empty span as an empty-string code segment, not dropped", () => {
    expect(splitTitleSegments("before `` after")).toEqual([
      { text: "before ", code: false },
      { text: "", code: true },
      { text: " after", code: false },
    ]);
  });

  it("never throws on edge-case input and never drops the trailing unpaired backtick", () => {
    const edgeCases = ["", "`", "``", "```", "a`b`c`d"];
    for (const title of edgeCases) {
      expect(() => splitTitleSegments(title)).not.toThrow();
    }
    // Odd number of backticks (3): the first pair is code, the leftover 3rd backtick is never
    // separated out — it stays attached to the rest of the string as literal plain text.
    expect(splitTitleSegments("a`b`c`d")).toEqual([
      { text: "a", code: false },
      { text: "b", code: true },
      { text: "c`d", code: false },
    ]);
  });
});

describe("stripBackticks", () => {
  it("removes paired backticks but keeps their contents as plain text", () => {
    expect(stripBackticks("Duplicate component label in `/etc/alloy` kills telemetry")).toBe(
      "Duplicate component label in /etc/alloy kills telemetry",
    );
  });

  it("leaves an unpaired backtick untouched", () => {
    expect(stripBackticks("totals ~40%`")).toBe("totals ~40%`");
  });

  it("returns plain titles unchanged", () => {
    expect(stripBackticks("Arc onboarding gotchas")).toBe("Arc onboarding gotchas");
  });
});
