import type { EdgeSingular, NodeSingular, StylesheetJsonBlock } from "cytoscape";
import type { TypeName } from "../../lib/types";

// Cytoscape styles are plain JS objects, not CSS — so this is the one place that bridges
// tokens.css (the single source of truth for both themes) into the values Cytoscape paints
// with. Nothing here hardcodes a colour: every value is read from a custom property at
// build time via getComputedStyle, and watchThemeChange tells the caller when to rebuild
// (theme toggle, or the OS-level scheme changing under an "auto" theme).

const TYPE_TOKENS: Record<TypeName, string> = {
  state: "--type-state",
  decision: "--type-decision",
  gotcha: "--type-gotcha",
  procedure: "--type-procedure",
  issue: "--type-issue",
  incident: "--type-incident",
  note: "--type-note",
  memory: "--type-memory",
};

const ROLE_TOKENS: Record<string, string> = {
  link: "--role-link",
  about: "--role-about",
  specializes: "--role-specializes",
  supersedes: "--role-supersedes",
  derives: "--role-derives",
  owns: "--role-owns",
};

export interface GraphPalette {
  ink: string;
  inkBody: string;
  ink2: string;
  ink3: string;
  surface: string;
  surface2: string;
  line: string;
  lineStrong: string;
  accent: string;
  onAccent: string;
  fontUi: string;
  types: Record<TypeName, string>;
  roles: Record<string, string>;
}

// This only fires when getComputedStyle itself throws — real browsers always have it, so in
// practice this is an SSR/exotic-test-environment guard, not a real rendering path. It
// deliberately does NOT encode a second designed palette (every type/role would need its own
// invented colour, which is exactly the "second hardcoded palette" tokens.css exists to
// prevent — see tests/guard/no-hex.test.ts). Instead it falls back to CSS system-color
// keywords: the platform's own notion of foreground/background/accent, with every type and
// every role collapsing to the same neutral pair. That is honest about what this state is —
// total degradation, not a themed alternative — and keeps every literal colour in this file
// confined to tokens.css as required.
const FALLBACK: GraphPalette = {
  ink: "CanvasText",
  inkBody: "CanvasText",
  ink2: "GrayText",
  ink3: "GrayText",
  surface: "Canvas",
  surface2: "Canvas",
  line: "GrayText",
  lineStrong: "GrayText",
  accent: "Highlight",
  onAccent: "HighlightText",
  fontUi: "system-ui, sans-serif",
  types: {
    state: "GrayText",
    decision: "GrayText",
    gotcha: "GrayText",
    procedure: "GrayText",
    issue: "GrayText",
    incident: "GrayText",
    note: "GrayText",
    memory: "GrayText",
  },
  roles: {
    link: "GrayText",
    about: "GrayText",
    specializes: "GrayText",
    supersedes: "GrayText",
    derives: "GrayText",
    owns: "GrayText",
  },
};

/** Reads the live custom-property values off `:root` (or a caller-supplied element, mainly
 * for tests). Falls back to FALLBACK's hardcoded copies only when getComputedStyle itself
 * is unavailable (jsdom without a real style engine) — never as a silent substitute for a
 * token that's merely missing, so a renamed token in tokens.css shows up as a visibly wrong
 * colour rather than a passing test. */
export function readGraphPalette(root: Element = document.documentElement): GraphPalette {
  let styles: CSSStyleDeclaration;
  try {
    styles = getComputedStyle(root);
  } catch {
    return FALLBACK;
  }
  const get = (name: string, fallback: string): string => {
    const value = styles.getPropertyValue(name).trim();
    return value.length > 0 ? value : fallback;
  };

  const types = {} as Record<TypeName, string>;
  for (const [type, token] of Object.entries(TYPE_TOKENS) as [TypeName, string][]) {
    types[type] = get(token, FALLBACK.types[type]);
  }
  const roles: Record<string, string> = {};
  for (const [role, token] of Object.entries(ROLE_TOKENS)) {
    roles[role] = get(token, FALLBACK.roles[role]);
  }

  return {
    ink: get("--ink", FALLBACK.ink),
    inkBody: get("--ink-body", FALLBACK.inkBody),
    ink2: get("--ink-2", FALLBACK.ink2),
    ink3: get("--ink-3", FALLBACK.ink3),
    surface: get("--surface", FALLBACK.surface),
    surface2: get("--surface-2", FALLBACK.surface2),
    line: get("--line", FALLBACK.line),
    lineStrong: get("--line-strong", FALLBACK.lineStrong),
    accent: get("--accent", FALLBACK.accent),
    onAccent: get("--on-accent", FALLBACK.onAccent),
    fontUi: get("--font-ui", FALLBACK.fontUi),
    types,
    roles,
  };
}

/**
 * Fires `onChange` whenever the resolved theme could have changed: the OS-level
 * prefers-color-scheme flipping (relevant whenever theme.ts leaves the app on "auto"), or
 * `data-theme` being written to <html> (theme.ts's explicit light/dark override). Returns
 * one combined unsubscribe. Guarded for environments without matchMedia/MutationObserver
 * (unit tests under jsdom) so importing this module never throws there.
 */
export function watchThemeChange(onChange: () => void): () => void {
  const unsubscribers: Array<() => void> = [];

  if (typeof window !== "undefined" && typeof window.matchMedia === "function") {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const listener = () => onChange();
    if (typeof media.addEventListener === "function") {
      media.addEventListener("change", listener);
      unsubscribers.push(() => media.removeEventListener("change", listener));
    }
  }

  if (typeof MutationObserver !== "undefined" && typeof document !== "undefined") {
    const observer = new MutationObserver((mutations) => {
      if (mutations.some((m) => m.attributeName === "data-theme")) onChange();
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    unsubscribers.push(() => observer.disconnect());
  }

  return () => unsubscribers.forEach((fn) => fn());
}

const HUB_ZOOM_THRESHOLD = 1.1;

// Cytoscape.js's `min-zoomed-font-size` does not do what an earlier version of this file
// assumed. Per Cytoscape's own docs it is a hide-below-threshold gate ("if zooming makes the
// effective font size of the label smaller than this, then no label is shown") — not a floor
// that keeps a label's on-screen size from shrinking. Confirmed live: at 1024x768,
// fit-to-content settles around zoom~0.43 and *every* label in the graph — scope titles and
// hub labels alike — silently rendered nothing, despite text-opacity computing to 1 for all
// of them, purely because that property was hiding them. A real floor has to be built by
// hand: font-size below is a function of the *current* zoom, so the rendered (on-screen) size
// stays pinned at the target size no matter how far fit-to-content had to zoom out.
// useCytoscapeGraph.ts's zoom/pan handler and its post-fit settle pass both call
// `cy.style().update()` to force this function to re-run, since Cytoscape does not
// re-evaluate function-valued styles on its own as zoom/pan changes — only on data/class
// changes or an explicit `update()`.
const MIN_ZOOM_FOR_FONT_SCALING = 0.05;

// Uncapped, this creates a runaway loop with useCytoscapeGraph.ts's settleFit: cy.fit()'s
// bounding box includes label geometry, so a bigger (zoom-compensated) model-space font
// makes the box bigger, which makes the next fit() zoom out further, which (being a smaller
// zoom) makes the compensated font bigger still — measured live, three settle passes alone
// dragged a real fit from zoom~0.43 down to zoom~0.16 with every hub label then colliding
// and getting suppressed. Capping how large the model-space font is allowed to grow breaks
// the loop: once zoom drops far enough that target/zoom would exceed the cap, the font stops
// responding to further zoom changes, so the next fit() sees an unchanging box and settles
// immediately instead of spiralling. The cap (~2.5x the target) sits well below any zoom this
// graph's corpus actually produces (measured 0.43-0.61 across the required viewports), so in
// practice full compensation is what's visible; it only engages as a circuit-breaker if a
// future re-layout or a much bigger corpus pushes fit-to-content zoom lower than that.
function zoomCompensatedFontSize(targetPx: number): (el: NodeSingular) => number {
  const maxPx = targetPx * 2.5;
  return (el) => Math.min(maxPx, targetPx / Math.max(el.cy().zoom(), MIN_ZOOM_FOR_FONT_SCALING));
}

/**
 * Cytoscape's own stylesheet cascade (later matching rules win) does the label-density
 * work no runtime code needs to touch per-frame: a base rule hides every label, `.zoomed-in`
 * (toggled by useCytoscapeGraph's `zoom`/`pan` handler comparing cy.zoom() against
 * HUB_ZOOM_THRESHOLD) shows all of them back, and `[?labelAlways]` (data flag set per-element
 * by graphElements.ts) wins over both — a hub or the ego centre keeps its label even
 * zoomed out, everything else appears only once you've zoomed in close enough to read it
 * without crowding.
 */
export function buildGraphStylesheet(palette: GraphPalette): StylesheetJsonBlock[] {
  return [
    {
      selector: "node.graph-scope",
      style: {
        shape: "round-rectangle",
        "background-color": palette.surface2,
        "background-opacity": 0.6,
        "border-width": 1.5,
        "border-color": palette.line,
        "border-style": "dashed",
        label: "data(label)",
        "text-valign": "top",
        "text-halign": "center",
        "font-family": palette.fontUi,
        // Scope labels are always on (no text-opacity toggle below) — zoomCompensatedFontSize
        // keeps them at a legible on-screen size even when the fit-to-content zoom is well
        // under 1, instead of shrinking in lockstep (or vanishing outright — see the comment
        // above HUB_ZOOM_THRESHOLD) with the rest of the canvas.
        "font-size": zoomCompensatedFontSize(15),
        "font-weight": 600,
        color: palette.ink2,
        "text-margin-y": -6,
        // Trimmed from 28px: this is pure margin between a scope's outer box and its
        // children's own footprint, on all four sides — at 8 nodes it was a visibly larger
        // empty border than the cluster needed, and it's one of the few knobs here that
        // shrinks a compound's box without touching node/label size or layout spacing.
        padding: "18px",
        // Cytoscape's compound auto-sizing defaults to "include" here — the scope box is
        // sized to fit every descendant's label bounding box regardless of that label's
        // text-opacity. Since almost all node labels are invisible at rest (graphStyle's own
        // density cascade below), that default was inflating every scope box to the width of
        // its widest hidden title (e.g. "Recreating an AKS node pool drops custom taints"),
        // which was large enough that adjacent scope boxes overlapped on screen even though
        // the node circles inside them never did. "exclude" sizes the box to the children's
        // actual rendered geometry (nodes + padding), which is what a compound cluster
        // boundary should reflect.
        "compound-sizing-wrt-labels": "exclude",
      },
    },
    {
      selector: "node.graph-node",
      style: {
        shape: "ellipse",
        width: "data(size)",
        height: "data(size)",
        "background-color": (el: NodeSingular) =>
          palette.types[el.data("type") as TypeName] ?? palette.accent,
        "border-width": 2,
        "border-color": palette.surface,
        label: "data(label)",
        "font-family": palette.fontUi,
        // Whichever labels the density cascade below turns on (hubs/centre at rest, every
        // label once zoomed past HUB_ZOOM_THRESHOLD) must still be readable at whatever zoom
        // fit-to-content settled on — zoomCompensatedFontSize pins their on-screen size at
        // 14px regardless of how zoomed out the view is, so "fewer labels, but legible"
        // actually holds at rest instead of the labels being hidden outright.
        "font-size": zoomCompensatedFontSize(14),
        color: palette.ink,
        "text-valign": "bottom",
        "text-halign": "center",
        "text-margin-y": 4,
        "text-background-color": palette.surface,
        "text-background-opacity": 0.85,
        "text-background-padding": "2px",
        "text-opacity": 0,
        "transition-property": "background-color, border-color",
        "transition-duration": 150,
      },
    },
    {
      selector: "node.graph-node-centre",
      style: {
        "border-width": 4,
        "border-color": palette.accent,
      },
    },
    // Label-density cascade: base hide, `.zoomed-in` shows everything, `[?labelAlways]`
    // (a boolean data flag — the `?` presence-selector) always wins regardless of zoom.
    { selector: "node.graph-node.zoomed-in", style: { "text-opacity": 1 } },
    { selector: "node.graph-node[?labelAlways]", style: { "text-opacity": 1 } },
    {
      selector: "node.graph-node:selected, node.graph-node.graph-node-hover",
      style: { "border-color": palette.accent, "border-width": 3, "text-opacity": 1 },
    },
    {
      selector: "node.graph-node.graph-node-dim",
      style: { opacity: 0.35 },
    },
    {
      selector: "edge.graph-edge",
      style: {
        width: 1.5,
        "curve-style": "bezier",
        "line-color": (el: EdgeSingular) => palette.roles[el.data("kind")] ?? palette.line,
        "target-arrow-color": (el: EdgeSingular) => palette.roles[el.data("kind")] ?? palette.line,
        "target-arrow-shape": "triangle",
        "arrow-scale": 0.8,
        opacity: 0.75,
      },
    },
    {
      selector: 'edge.graph-edge[kind = "link"]',
      style: { "line-style": "dashed" },
    },
    {
      selector: "edge.graph-edge.graph-edge-dim",
      style: { opacity: 0.12 },
    },
    {
      selector: "edge.graph-edge.graph-edge-hover",
      style: { width: 3, opacity: 1 },
    },
  ];
}

export { HUB_ZOOM_THRESHOLD };
