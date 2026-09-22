// cytoscape-fcose ships no type definitions of its own. Its default export is a Cytoscape
// extension-registration function — exactly cytoscape's own `Ext` shape
// (`type Ext = (cy: typeof cytoscape) => void;`, node_modules/cytoscape/index.d.ts), which is
// what `cytoscape.use(fcose)` expects. See cytoscape-fcose's README for the registration
// pattern this shim exists to type: `import fcose from "cytoscape-fcose"; cytoscape.use(fcose);`.
declare module "cytoscape-fcose" {
  import type cytoscape from "cytoscape";

  const fcose: cytoscape.Ext;
  export default fcose;
}
