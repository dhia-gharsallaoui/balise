// The cytoscape-layout-utilities extension (see cytoscape-layout-utilities.d.ts next to this
// file) adds an instance method to every `cy` core once registered —
// `cy.layoutUtilities(options)` (see that package's README "API" section) — which isn't part
// of cytoscape's own shipped types (node_modules/cytoscape/index.d.ts), since cytoscape has
// no idea this extension exists. Augment `Core` with the method and the subset of its
// options this codebase actually passes (README's "Default Options": idealEdgeLength,
// offset, desiredAspectRatio, polyominoGridSizeFactor, utilityFunction, componentSpacing) so
// configureComponentPacking in useCytoscapeGraph.ts type-checks without `any`.
//
// This top-level `export {}` is load-bearing, not decorative: it's what makes TypeScript
// treat this file as a module, which in turn makes the `declare module "cytoscape"` block
// below an *augmentation* of the real, already-typed "cytoscape" module rather than a
// second, competing ambient declaration of that name. Without it, TypeScript silently
// discards every member cytoscape's own types define on `Core` (zoom, elements, on, ...
// all of it) instead of adding `layoutUtilities` alongside them — confirmed live: `npx tsc
// -b` went from 1 error (the missing method) to 62 (every other Core member gone) the moment
// this augmentation was added to a script-mode file. Keeping this augmentation in its own
// module-mode file (rather than folding it into cytoscape-layout-utilities.d.ts, which must
// stay script-mode for its own unrelated ambient module declaration to resolve) is what lets
// both declarations coexist correctly.
export {};

declare module "cytoscape" {
  interface LayoutUtilitiesOptions {
    idealEdgeLength?: number;
    offset?: number;
    desiredAspectRatio?: number;
    polyominoGridSizeFactor?: number;
    utilityFunction?: 1 | 2;
    componentSpacing?: number;
  }

  interface Core {
    layoutUtilities(options?: LayoutUtilitiesOptions): void;
  }
}
