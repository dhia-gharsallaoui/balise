---
uid: 01JAAAAAAAAAAAAAAAAAAAAAA2
slug: globex-er-circuit
type: state
scope: client-globex
title: Globex primary ExpressRoute circuit
# instance_of is a deliberate cross-scope reference (client-globex -> work) and is expected
# to dangle. Slugs are unique only per (scope, slug), never globally, so a bare ref like
# this one is genuinely ambiguous the moment two scopes each hold a page slugged
# "expressroute" — resolving it here would mean guessing. The real fix is a
# scope-qualified reference form (e.g. instance_of: work/expressroute) or a declared
# shared scope; 04 §4 does not define either yet, so this stays a documented gap rather
# than a hidden one.
instance_of: expressroute
superseded_by: globex-er-v1
about: [globex-er-v1]
tags: [customer/globex, vendor/azure/expressroute, layer/network]
as_of: 2026-08-01
claims:
  - {text: The Globex ExpressRoute circuit runs at 10 Gbps, status: active, id: c1}
  - {text: Peering uses a private ASN on the customer side, status: active, id: c2}
status: active
owner: dhia
last_verified: 2026-08-30
---
The circuit connects the Globex on-premises hub to the Azure ExpressRoute gateway.
It specializes the general ExpressRoute entity — a cross-scope reference left
deliberately dangling until the spec defines a qualified-ref form — and it has since
been superseded by a higher-capacity circuit, which is what this page is about.
