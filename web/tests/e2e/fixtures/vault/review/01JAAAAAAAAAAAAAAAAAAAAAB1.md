---
id: 01JAAAAAAAAAAAAAAAAAAAAAB1
kind: claims
scope: work
target: azapi-ergw-connection-deletion
confidence: 0.82
created_by: fixture-vault
evidence:
  - source: work/gotchas/azapi-ergw-connection-deletion.md
    span: [20, 25]
    text: "Any PUT on an ExpressRoute gateway through the AzAPI provider replaces the resource."
change:
  claims:
    keep: [c1, c2, c3, c5]
    reword:
      - id: c4
        text: Prefer azapi_update_resource, which patches the gateway in place instead of replacing it
    retire: []
    add:
      - text: The upstream provider fix has not shipped as of this review; keep re-creating connections manually
        status: active
        span: [20, 21]
status: pending
---
Proposed reword of c4 for precision, plus a new claim capturing that the workaround is still
required. No claims are being retired here — c6 already documents the superseded workaround
status and this proposal does not touch it.
