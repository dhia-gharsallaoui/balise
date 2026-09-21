---
uid: 01JAAAAAAAAAAAAAAAAAAAAAA1
slug: azapi-ergw-connection-deletion
type: gotcha
scope: work
title: AzAPI ER gateway update DELETES its connections
aliases: [ergw-connection-deletion]
tags: [vendor/azure/expressroute, layer/network]
claims:
  - {text: Updating the gateway through AzAPI removes every ExpressRoute connection, status: active, id: c1}
  - {text: The Terraform plan shows nothing to destroy before it happens, status: active, id: c2}
  - {text: Re-create the connections after any gateway update, status: active, id: c3}
  - {text: Prefer azapi_update_resource which patches in place, status: active, id: c4}
  - {text: Treat any gateway update as a connection outage, status: active, id: c5}
  - {text: Workaround was a manual re-create before the provider fix, status: superseded, as_of: 2026-06-10, id: c6}
status: active
owner: dhia
last_verified: 2026-08-30
---
Any PUT on an ExpressRoute gateway through the AzAPI provider replaces the resource.

## What to do

- Apply during a window, then re-run the connection stage.
- Prefer `azapi_update_resource`, which patches instead of replacing.
