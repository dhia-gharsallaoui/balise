import { describe, expect, it } from "vitest";
import { resolveTheme } from "../../src/lib/theme";

describe("resolveTheme", () => {
  it("follows the system when nothing is stored", () => {
    expect(resolveTheme(null, true)).toBe("dark");
    expect(resolveTheme(null, false)).toBe("light");
  });

  it("lets an explicit choice override the system", () => {
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
  });

  it("falls back to the system when the stored value is nonsense", () => {
    expect(resolveTheme("chartreuse", true)).toBe("dark");
  });

  it("treats an explicit system choice as following the system", () => {
    expect(resolveTheme("system", true)).toBe("dark");
  });
});
