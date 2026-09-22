package main

// acmePages is the "client-acme" scope: Acme's retail SD-WAN fabric, the smallest of the
// three client scopes.
func acmePages() []page {
	return []page{
		{
			UID: uid(17), Slug: "acme-sdwan-fabric-primary", Type: "state", Scope: "client-acme",
			Title: "Acme SD-WAN fabric, primary controller generation",
			About: []string{"acme-fortigate-ha-failover-drops-sdwan-sla"},
			Tags:  []string{"customer/acme", "layer/fabric", "vendor/fortinet"},
			AsOf:  "2026-08-01",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Primary controller pair manages every retail site's SD-WAN tunnel set"},
				{ID: "c2", Status: "active", Text: "HA failover between controllers takes under ten seconds in steady state"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-01",
			Body: "The primary controller generation manages tunnel state for every Acme retail " +
				"site. [[acme-fortigate-ha-failover-drops-sdwan-sla]] documents a known failover " +
				"gap this generation still has.\n",
		},
		{
			UID: uid(18), Slug: "acme-fortigate-ha-failover-drops-sdwan-sla", Type: "gotcha", Scope: "client-acme",
			Title: "FortiGate HA failover drops SD-WAN SLA state",
			Tags:  []string{"customer/acme", "layer/fabric", "vendor/fortinet"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Failover resets per-link SLA history, so SD-WAN rebalances as if links are new"},
				{ID: "c2", Status: "active", Text: "Rebalance settles within two minutes but briefly favors the wrong link"},
				{ID: "c3", Status: "active", Text: "Affects every site during Black Friday-scale concurrent failover events"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-10",
			Body: "When the standby FortiGate takes over, it starts SD-WAN SLA measurement from " +
				"zero for every link instead of inheriting the active unit's history. For roughly " +
				"two minutes the site can route over a link that would normally have been " +
				"deprioritized.\n\n" +
				"Seen most visibly during [[acme-black-friday-fabric-saturation-2026]], where many " +
				"sites failed over within the same hour. Related noise pattern: " +
				"[[acme-fabric-monitoring-alert-noise]].\n",
		},
		{
			UID: uid(19), Slug: "acme-vlan-reuse-across-sites-conflict", Type: "gotcha", Scope: "client-acme",
			Title: "Reused VLAN IDs across retail sites conflict at the regional hub",
			Tags:  []string{"customer/acme", "layer/network"},
			Claims: []claim{
				{ID: "c1", Status: "superseded", AsOf: "2026-08-25", Text: "Two sites sharing VLAN 120 caused a routing loop at the regional hub"},
				{ID: "c2", Status: "active", Text: "Resolved by the site-local VLAN scheme; no longer reachable under current design"},
			},
			Status: "retired", Owner: "Priya Nair", LastVerified: "2026-08-25",
			Body: "Before [[acme-standardize-site-vlan-scheme]], two retail sites reusing the " +
				"same VLAN ID could both land traffic at the regional hub and create a routing " +
				"loop. The new scheme makes the ID collision structurally impossible, so this " +
				"page is retired rather than actively maintained.\n",
		},
		{
			UID: uid(20), Slug: "acme-standardize-site-vlan-scheme", Type: "decision", Scope: "client-acme",
			Title: "Standardize Acme retail site VLAN numbering",
			Tags:  []string{"customer/acme", "layer/network"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "VLAN ID now derives from a site's four-digit store code, never reused"},
				{ID: "c2", Status: "active", Text: "Closes the conflict class described in the retired VLAN reuse gotcha"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-08-26",
			Body: "Every retail site's VLAN ID is now derived deterministically from its " +
				"four-digit store code, closing the collision class in " +
				"[[acme-vlan-reuse-across-sites-conflict]]. [[acme-onboard-new-retail-site]] " +
				"applies this scheme as its first step.\n",
		},
		{
			UID: uid(21), Slug: "acme-onboard-new-retail-site", Type: "procedure", Scope: "client-acme",
			Title: "Onboard a new Acme retail site to the fabric",
			Tags:  []string{"customer/acme", "layer/network", "layer/fabric"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Assign the site VLAN ID from its store code before any cabling work"},
				{ID: "c2", Status: "active", Text: "Register the new tunnel with the primary controller pair, not a standby"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-05",
			Body: "1. Assign the site's VLAN ID per [[acme-standardize-site-vlan-scheme]].\n" +
				"2. Register the new SD-WAN tunnel with [[acme-sdwan-fabric-primary]].\n" +
				"3. Confirm SLA measurement has accumulated at least one hour of history before " +
				"declaring the site live.\n",
		},
		{
			UID: uid(22), Slug: "acme-fabric-monitoring-alert-noise", Type: "issue", Scope: "client-acme",
			Title: "Fabric monitoring pages on every HA failover, not just bad ones",
			Tags:  []string{"customer/acme", "layer/fabric", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Alert fires on any failover event regardless of whether SLA was actually breached"},
				{ID: "c2", Status: "active", Text: "Threshold change proposed: page only when SLA breach exceeds two minutes"},
			},
			Status: "resolved", Owner: "Priya Nair", LastVerified: "2026-09-11",
			Body: "The fabric monitor pages on-call for every HA failover event, including the " +
				"benign rebalance window described in " +
				"[[acme-fortigate-ha-failover-drops-sdwan-sla]]. Tightening the alert threshold " +
				"to only page past a two-minute SLA breach closed most of the noise.\n",
		},
		{
			UID: uid(23), Slug: "acme-black-friday-fabric-saturation-2026", Type: "incident", Scope: "client-acme",
			Title: "Black Friday fabric saturation, 2026",
			Tags:  []string{"customer/acme", "layer/fabric"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Forty sites failed over within the same hour under peak POS traffic"},
				{ID: "c2", Status: "active", Text: "SLA rebalance noise triggered the paging storm described in the linked issue"},
				{ID: "c3", Status: "active", Text: "No site lost connectivity; the incident was entirely alert volume"},
			},
			Status: "resolved", Owner: "Priya Nair", LastVerified: "2026-09-11",
			Body: "Roughly forty retail sites failed over within the same hour under Black " +
				"Friday point-of-sale load, each briefly re-running SLA measurement per " +
				"[[acme-fortigate-ha-failover-drops-sdwan-sla]]. The result was a paging storm " +
				"rather than an outage: [[acme-fabric-monitoring-alert-noise]] tracks the alert " +
				"threshold fix that followed.\n",
		},
		{
			UID: uid(24), Slug: "acme-account-escalation-contacts", Type: "note", Scope: "client-acme",
			Title: "Acme account escalation contacts",
			Tags:  []string{"customer/acme"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Acme's primary contact is their retail infrastructure manager, paged via SMS"},
				{ID: "c2", Status: "active", Text: "After-hours escalation requires a named approver, no group inbox accepted"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-14",
			Body: "Acme's primary contact is their retail infrastructure manager, reachable by " +
				"SMS for anything site-affecting. After-hours changes require a named approver " +
				"on their side; a shared team inbox reply is not treated as approval.\n",
		},
	}
}
