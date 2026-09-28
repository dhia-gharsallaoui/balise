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
  fontMono: string;
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
  fontMono: "ui-monospace, monospace",
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
    fontMono: get("--font-mono", FALLBACK.fontMono),
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
function zoomCompensated(targetPx: number): (el: NodeSingular) => number {
  const maxPx = targetPx * 2.5;
  return (el) => Math.min(maxPx, targetPx / Math.max(el.cy().zoom(), MIN_ZOOM_FOR_FONT_SCALING));
}

function zoomCompensatedFontSize(targetPx: number): (el: NodeSingular) => number {
  return zoomCompensated(targetPx);
}

// A node label is capped at this many on-screen px and ellipsised past it. Uncapped, one long
// claim-style title ("Every generation gets a permanent run_id recording full provenance")
// painted a 400px strip across its neighbours and off the canvas edge. The cap is
// zoom-compensated exactly like the font, so it holds on screen whatever fit-to-content picked;
// the full title is one hover away (the label cascade shows it) and always in the a11y list.
const LABEL_MAX_WIDTH_PX = 160;

// Geometry (disc size, ring width, edge width) that must not *grow* on screen past its model
// size when the user zooms in, or when a small vault fit-zooms in to fill the frame: the
// graph spreads out, the marks stay refined. Below zoom 1 it scales down normally, since the
// layout packed nodes assuming their model size and inflating them would make them overlap.
type Sized = NodeSingular | EdgeSingular;

function noGrow(px: number): (el: Sized) => number {
  return (el) => px / Math.max(el.cy().zoom(), 1);
}

function nodeDiameter(el: NodeSingular): number {
  return noGrow(Number(el.data("size")) || 0)(el);
}

function zoomCompensatedMaxWidth(targetPx: number): (el: NodeSingular) => string {
  const scale = zoomCompensated(targetPx);
  return (el) => `${scale(el)}px`;
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
        "corner-radius": "10px",
        "background-color": palette.surface2,
        "background-opacity": 0.35,
        "border-width": (el: NodeSingular) => noGrow(1)(el),
        "border-color": palette.lineStrong,
        "border-style": "dashed",
        label: "data(label)",
        "text-valign": "top",
        "text-halign": "center",
        // A scope is a quiet container, not a heading: mono (scopes are identifiers
        // everywhere else in the app) at meta size in ink-3.
        "font-family": palette.fontMono,
        // Always on (no text-opacity toggle below): zoomCompensatedFontSize keeps the title at
        // a fixed on-screen size even when the fit-to-content zoom is well under 1, instead of
        // shrinking in lockstep (or vanishing, see the comment above HUB_ZOOM_THRESHOLD).
        "font-size": zoomCompensatedFontSize(12),
        "font-weight": 400,
        color: palette.ink3,
        "text-margin-y": -6,
        // Pure margin between the scope's outer box and its children's own footprint.
        padding: "18px",
        // Cytoscape's compound auto-sizing defaults to "include": the scope box would grow to
        // fit every descendant's label box whether or not that label is painted, which made
        // adjacent scopes overlap on screen. "exclude" sizes it to the children's rendered
        // geometry (nodes + padding), which is what a cluster boundary should reflect.
        "compound-sizing-wrt-labels": "exclude",
      },
    },
    // A small graph shows every label (useCytoscapeGraph's label-aware layout), so there the
    // scope box should wrap them too, instead of titles spilling over its dashed edge.
    {
      selector: "node.graph-scope.graph-scope-fit-labels",
      style: { "compound-sizing-wrt-labels": "include" },
    },
    {
      selector: "node.graph-node",
      style: {
        shape: "ellipse",
        width: nodeDiameter,
        height: nodeDiameter,
        "background-color": (el: NodeSingular) =>
          palette.types[el.data("type") as TypeName] ?? palette.accent,
        // A thin surface-coloured ring separates a disc from edges and from an overlapping
        // neighbour, the way a map marker is cut out of the terrain under it.
        "border-width": noGrow(2),
        "border-color": palette.surface,
        label: "data(label)",
        "font-family": palette.fontUi,
        // Whichever labels the density cascade below turns on must be readable at whatever zoom
        // fit-to-content settled on: zoomCompensatedFontSize pins their on-screen size at 12px.
        "font-size": zoomCompensatedFontSize(12),
        "font-weight": 500,
        color: palette.inkBody,
        "text-valign": "bottom",
        "text-halign": "center",
        // Clears the disc plus its widest (selected/centre) ring and the label plate's padding.
        "text-margin-y": noGrow(8),
        "text-wrap": "ellipsis",
        "text-max-width": zoomCompensatedMaxWidth(LABEL_MAX_WIDTH_PX),
        // Labels sit on a surface-coloured plate so an edge running behind them never cuts
        // through the text.
        "text-background-color": palette.surface,
        "text-background-opacity": 0.92,
        "text-background-padding": (el: NodeSingular) => `${noGrow(3)(el)}px`,
        "text-background-shape": "roundrectangle",
        "text-opacity": 0,
        "transition-property": "border-color, border-width, opacity",
        "transition-duration": 140,
      },
    },
    {
      selector: "node.graph-node-centre",
      style: {
        "border-width": noGrow(3),
        "border-color": palette.ink,
      },
    },
    // Label-density cascade: base hide, `.zoomed-in` shows everything, `[?labelAlways]`
    // (a boolean data flag, the `?` presence-selector) always wins regardless of zoom, and
    // `[?labelSuppressed]` (set at runtime by useCytoscapeGraph's collision pass) hides a label
    // that would print over another. Hover/selection, last, always shows its own label.
    { selector: "node.graph-node.zoomed-in", style: { "text-opacity": 1 } },
    { selector: "node.graph-node[?labelAlways]", style: { "text-opacity": 1 } },
    { selector: "node.graph-node[?labelSuppressed]", style: { "text-opacity": 0 } },
    {
      selector: "node.graph-node:selected, node.graph-node.graph-node-hover",
      style: {
        "border-color": palette.ink,
        "border-width": noGrow(3),
        "text-opacity": 1,
        color: palette.ink,
        "z-index": 20,
      },
    },
    {
      selector: "node.graph-node.graph-node-dim",
      style: { opacity: 0.3 },
    },
    {
      selector: "edge.graph-edge",
      style: {
        width: noGrow(1.25),
        "curve-style": "bezier",
        "line-color": (el: EdgeSingular) => palette.roles[el.data("kind")] ?? palette.lineStrong,
        "target-arrow-color": (el: EdgeSingular) => palette.roles[el.data("kind")] ?? palette.lineStrong,
        "target-arrow-shape": "triangle",
        "arrow-scale": 0.7,
        opacity: 0.8,
      },
    },
    {
      selector: 'edge.graph-edge[kind = "link"]',
      style: { "line-style": "dashed", "line-dash-pattern": [4, 3] },
    },
    {
      selector: "edge.graph-edge.graph-edge-dim",
      style: { opacity: 0.1 },
    },
    {
      selector: "edge.graph-edge.graph-edge-hover",
      style: { width: noGrow(2), opacity: 1 },
    },
  ];
}

export { HUB_ZOOM_THRESHOLD };
