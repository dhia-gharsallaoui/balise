import { afterEach, describe, expect, it } from "vitest";
import type { StylesheetJsonBlock, StylesheetStyle } from "cytoscape";
import {
  buildGraphStylesheet,
  HUB_ZOOM_THRESHOLD,
  readGraphPalette,
  watchThemeChange,
} from "../../src/components/knowledge/graphStyle";

// StylesheetJsonBlock is a union (StylesheetStyle | StylesheetCSS) because Cytoscape also
// accepts raw CSS-string blocks — buildGraphStylesheet never emits those, it only ever emits
// the `{ selector, style }` object form, so this narrows for the tests rather than weakening
// buildGraphStylesheet's own return type just to make assertions convenient.
function styleOf(rule: StylesheetJsonBlock | undefined): Record<string, unknown> {
  if (!rule || !("style" in rule)) {
    throw new Error("expected a StylesheetStyle rule (with .style), got a StylesheetCSS block or nothing");
  }
  return (rule as StylesheetStyle).style as Record<string, unknown>;
}

// readGraphPalette reads custom properties off a real element via getComputedStyle — jsdom
// supports both, so a <div> with inline `style` custom properties is enough to exercise the
// real (non-fallback) path, rather than only ever hitting FALLBACK.
function elementWithTokens(tokens: Record<string, string>): HTMLElement {
  const el = document.createElement("div");
  document.body.appendChild(el);
  for (const [name, value] of Object.entries(tokens)) {
    el.style.setProperty(name, value);
  }
  return el;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("readGraphPalette", () => {
  it("reads every declared custom property from the given element", () => {
    const el = elementWithTokens({
      "--ink": "#111111",
      "--surface": "#fefefe",
      "--accent": "#0000ff",
      "--type-gotcha": "#abcdef",
      "--role-about": "#123456",
    });
    const palette = readGraphPalette(el);
    expect(palette.ink).toBe("#111111");
    expect(palette.surface).toBe("#fefefe");
    expect(palette.accent).toBe("#0000ff");
    expect(palette.types.gotcha).toBe("#abcdef");
    expect(palette.roles.about).toBe("#123456");
  });

  it("falls back to the hardcoded value for any token that is missing, not thrown", () => {
    const el = elementWithTokens({ "--ink": "#222222" });
    const palette = readGraphPalette(el);
    expect(palette.ink).toBe("#222222");
    // --surface was never set on this element, so it must fall back rather than be empty.
    expect(palette.surface).toBeTruthy();
    expect(palette.surface).not.toBe("");
  });

  it("returns every TypeName token and every known role token", () => {
    const palette = readGraphPalette(document.createElement("div"));
    expect(Object.keys(palette.types).sort()).toEqual(
      ["state", "decision", "gotcha", "procedure", "issue", "incident", "entity", "note"].sort(),
    );
    expect(Object.keys(palette.roles).sort()).toEqual(
      ["link", "about", "specializes", "supersedes", "derives", "owns"].sort(),
    );
  });

  it("degrades to the FALLBACK palette without throwing when getComputedStyle is unavailable", () => {
    const original = window.getComputedStyle;
    // A zero-arg function is structurally assignable to getComputedStyle's signature (fewer
    // params is fine), so this needs no @ts-expect-error — it just type-checks.
    window.getComputedStyle = () => {
      throw new Error("no style engine");
    };
    try {
      expect(() => readGraphPalette(document.documentElement)).not.toThrow();
      const palette = readGraphPalette(document.documentElement);
      expect(palette.ink).toBe("CanvasText");
    } finally {
      window.getComputedStyle = original;
    }
  });
});

describe("buildGraphStylesheet", () => {
  const palette = readGraphPalette(elementWithTokens({}));

  it("produces a stylesheet with rules for scope nodes, page nodes, and edges", () => {
    const stylesheet = buildGraphStylesheet(palette);
    const selectors = stylesheet.map((rule) => rule.selector);
    expect(selectors).toContain("node.graph-scope");
    expect(selectors).toContain("node.graph-node");
    expect(selectors).toContain("edge.graph-edge");
  });

  it("hides labels by default and shows them once zoomed in or always-labelled", () => {
    const stylesheet = buildGraphStylesheet(palette);
    const base = styleOf(stylesheet.find((r) => r.selector === "node.graph-node"));
    expect(base["text-opacity"]).toBe(0);

    const zoomedIn = styleOf(stylesheet.find((r) => r.selector === "node.graph-node.zoomed-in"));
    expect(zoomedIn["text-opacity"]).toBe(1);

    const always = styleOf(stylesheet.find((r) => r.selector === "node.graph-node[?labelAlways]"));
    expect(always["text-opacity"]).toBe(1);
  });

  it("dims nodes/edges under .graph-node-dim / .graph-edge-dim for hover-neighbourhood highlighting", () => {
    const stylesheet = buildGraphStylesheet(palette);
    const dimNode = styleOf(stylesheet.find((r) => r.selector === "node.graph-node.graph-node-dim"));
    const dimEdge = styleOf(stylesheet.find((r) => r.selector === "edge.graph-edge.graph-edge-dim"));
    expect(dimNode.opacity as number).toBeLessThan(1);
    expect(dimEdge.opacity as number).toBeLessThan(dimNode.opacity as number);
  });

  it("colours a node by its type token via a mapping function, falling back to accent for an unknown type", () => {
    const stylesheet = buildGraphStylesheet(palette);
    const nodeRule = styleOf(stylesheet.find((r) => r.selector === "node.graph-node"));
    const bg = nodeRule["background-color"] as (el: unknown) => string;
    expect(typeof bg).toBe("function");
    expect(bg({ data: (key: string) => (key === "type" ? "gotcha" : undefined) })).toBe(palette.types.gotcha);
    expect(bg({ data: () => "not-a-real-type" })).toBe(palette.accent);
  });
});

describe("HUB_ZOOM_THRESHOLD", () => {
  it("is a finite positive number greater than 1 (a real zoom-in, not the resting zoom)", () => {
    expect(HUB_ZOOM_THRESHOLD).toBeGreaterThan(1);
    expect(Number.isFinite(HUB_ZOOM_THRESHOLD)).toBe(true);
  });
});

describe("watchThemeChange", () => {
  it("fires the callback when data-theme changes on <html>", async () => {
    let calls = 0;
    const unsubscribe = watchThemeChange(() => {
      calls += 1;
    });
    document.documentElement.setAttribute("data-theme", "dark");
    // MutationObserver callbacks run as a microtask.
    await Promise.resolve();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(calls).toBeGreaterThan(0);
    unsubscribe();
    document.documentElement.removeAttribute("data-theme");
  });

  it("returns an unsubscribe function that stops further callbacks", async () => {
    let calls = 0;
    const unsubscribe = watchThemeChange(() => {
      calls += 1;
    });
    unsubscribe();
    document.documentElement.setAttribute("data-theme", "dark");
    await Promise.resolve();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(calls).toBe(0);
    document.documentElement.removeAttribute("data-theme");
  });
});
