---
id: 01JAAAAAAAAAAAAAAAAAAAAAB2
kind: claims
scope: client-globex
target: globex-er-circuit
confidence: 0.74
created_by: fixture-vault
evidence:
  - source: client-globex/state/globex-er-circuit.md
    span: [26, 29]
    text: "The circuit connects the Globex on-premises hub to the Azure ExpressRoute gateway."
change:
  claims:
    keep: [c2]
    reword: []
    retire:
      - id: c1
        as_of: 2026-08-01
    add:
      - text: Capacity planning now lives on the successor circuit tracked at globex-er-v1
        status: active
        span: [27, 29]
status: pending
---
This page's own frontmatter already marks it superseded_by globex-er-v1; this proposal retires
c1 (the 10 Gbps figure) so the active claim set stops repeating a number that belongs to the
prior circuit, and points capacity questions at the successor page instead.
