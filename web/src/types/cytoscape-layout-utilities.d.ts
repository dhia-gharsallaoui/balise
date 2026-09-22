// cytoscape-layout-utilities ships no type definitions of its own, same situation as
// cytoscape-fcose (see cytoscape-fcose.d.ts next to this file) — its default export is a
// Cytoscape extension-registration function. fCoSE's `packComponents: true` option is a
// silent no-op unless this extension is also registered (see cytoscape-fcose's README:
// "cytoscape-layout-utilities extension should also be registered in the application" —
// without it, fCoSE lays out the whole graph, including every disconnected scope compound
// and every isolated node, as a single force pass with no post-hoc packing step, which is
// what left the global graph's fit-to-content bounding box several times larger than the
// container). Registering it gives fCoSE a real `cy.layoutUtilities` to call.
//
// Kept as a plain ambient-module script file (no top-level import/export), same as
// cytoscape-fcose.d.ts — that's what makes `declare module "cytoscape-layout-utilities"`
// below a brand-new global ambient module declaration. The `cy.layoutUtilities(...)`
// instance-method augmentation of the real "cytoscape" module lives in
// cytoscape-core-augment.d.ts instead of here: that augmentation needs its *own* file to be
// a proper ES module (a top-level `export {}`), and mixing the two declaration styles in one
// file — script-mode ambient declaration for an untyped package, alongside module-mode
// augmentation of an already-typed package — made TypeScript stop resolving one or the
// other (confirmed live: with both in one file, either the "cytoscape" Core members
// vanished, or this package's own default export stopped resolving with TS7016). Splitting
// them into two files, each in the mode it actually needs, fixed both at once.
declare module "cytoscape-layout-utilities" {
  import type cytoscape from "cytoscape";

  const layoutUtilities: cytoscape.Ext;
  export default layoutUtilities;
}
