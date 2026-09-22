package main

// globexCrossScopeDanglingComment explains, the same way defaults/fixtures/globex-er-circuit.md
// already does, why globex-expressroute-circuit-v2's instance_of is deliberately left pointing
// at a slug that cannot resolve: link resolution (internal/vault/links.go) is scoped strictly
// to (scope, slug), so a bare ref can never cross from client-globex into work, no matter how
// unambiguous the target name looks to a human reader.
const globexCrossScopeDanglingComment = "" +
	"# instance_of names work/expressroute-transit, the shared platform entity this circuit is\n" +
	"# really an instance of — but ref resolution never crosses scope boundaries, only slug\n" +
	"# lookup within this same scope, so the reference is left dangling on purpose rather than\n" +
	"# quietly rewritten to something that would resolve but mean something else.\n"

// globexPages is the "client-globex" scope: ExpressRoute circuit history plus an AKS/storage
// pair of gotchas, one issue and one incident tying the two together.
func globexPages() []page {
	return []page{
		{
			UID: uid(25), Slug: "globex-expressroute-circuit-v1", Type: "state", Scope: "client-globex",
			Title:        "Globex ExpressRoute circuit, v1",
			SupersededBy: "globex-expressroute-circuit-v2",
			Tags:         []string{"customer/globex", "layer/network", "vendor/azure/expressroute"},
			AsOf:         "2026-05-10",
			Claims: []claim{
				{ID: "c1", Status: "superseded", AsOf: "2026-08-30", Text: "Single 1 Gbps circuit terminating at the regional peering location"},
				{ID: "c2", Status: "superseded", AsOf: "2026-08-30", Text: "No secondary circuit; a single peering failure was a full outage risk"},
			},
			Status: "superseded", Owner: "Sana Idris", LastVerified: "2026-08-30",
			Body: "Globex's original single-circuit setup, replaced by " +
				"[[globex-expressroute-circuit-v2]] once the dual-circuit standard applied to " +
				"tier 2+ clients.\n",
		},
		{
			UID: uid(26), Slug: "globex-expressroute-circuit-v2", Type: "state", Scope: "client-globex",
			Title:      "Globex ExpressRoute circuit, v2",
			Aliases:    []string{"globex-er-v2"},
			Comment:    globexCrossScopeDanglingComment,
			InstanceOf: "expressroute-transit",
			About:      []string{"globex-expressroute-circuit-v1"},
			Tags:       []string{"customer/globex", "layer/network", "vendor/azure/expressroute"},
			AsOf:       "2026-08-30",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Dual 1 Gbps circuits across two peering locations, active-active"},
				{ID: "c2", Status: "active", Text: "Each circuit alone can carry full production load if the other fails"},
				{ID: "c3", Status: "active", Text: "Deployed under the platform's dual-circuit standard for tier 2+ clients"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-09",
			Body: "Current circuit pair, replacing [[globex-expressroute-circuit-v1]]. " +
				"[[globex-expressroute-circuit-capacity-upgrade]] is the procedure used to grow " +
				"either circuit's bandwidth without a peering location change.\n",
		},
		{
			UID: uid(27), Slug: "globex-storage-account-immutability-lock-blocks-lifecycle", Type: "gotcha", Scope: "client-globex",
			Title: "Immutability lock on a storage account blocks lifecycle deletes",
			Tags:  []string{"customer/globex", "layer/storage"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A time-based immutability lock silently blocks lifecycle-rule deletes too"},
				{ID: "c2", Status: "active", Text: "The lifecycle rule shows as applied even though nothing was deleted"},
				{ID: "c3", Status: "active", Text: "Locked blobs age out only when the immutability period itself expires"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-06",
			Body: "A storage account under a time-based immutability policy blocks deletes from " +
				"lifecycle rules the same way it blocks manual deletes, but the portal still " +
				"shows the rule as successfully applied, which makes the block easy to miss. " +
				"[[globex-storage-lifecycle-rules-not-running]] is the resulting issue.\n",
		},
		{
			UID: uid(28), Slug: "globex-aks-ingress-cert-renewal-race", Type: "gotcha", Scope: "client-globex",
			Title: "AKS ingress cert renewal races the load balancer health check",
			Tags:  []string{"customer/globex", "customer/globex/consumer", "layer/compute", "vendor/azure/aks"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "New cert loads before the health check re-probes, briefly serving a mismatch"},
				{ID: "c2", Status: "active", Text: "Window is under thirty seconds but wide enough to fail strict TLS clients"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-07",
			Body: "The ingress controller reloads a renewed certificate before the load " +
				"balancer's health check has re-probed the backend, so for a short window a " +
				"strict TLS client can see a certificate mismatch. " +
				"[[globex-move-to-managed-cert-renewal]] is the fix in progress; " +
				"[[globex-ingress-502-window-2026-07]] is the incident that first surfaced it.\n",
		},
		{
			UID: uid(29), Slug: "globex-move-to-managed-cert-renewal", Type: "decision", Scope: "client-globex",
			Title: "Move Globex ingress certs to managed renewal",
			Tags:  []string{"customer/globex", "layer/compute", "vendor/azure/aks"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Managed renewal staggers reload behind a health-check confirmation step"},
				{ID: "c2", Status: "active", Text: "Removes the manual renewal race described in the ingress cert gotcha"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-08",
			Body: "Replacing manual certificate renewal with a managed flow that waits for a " +
				"health-check confirmation before switching the load balancer to the new " +
				"certificate, closing the race in " +
				"[[globex-aks-ingress-cert-renewal-race]].\n",
		},
		{
			UID: uid(30), Slug: "globex-expressroute-circuit-capacity-upgrade", Type: "procedure", Scope: "client-globex",
			Title: "Upgrade Globex ExpressRoute circuit capacity",
			Tags:  []string{"customer/globex", "layer/network", "vendor/azure/expressroute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Request the bandwidth upgrade on one circuit at a time, never both together"},
				{ID: "c2", Status: "active", Text: "Confirm traffic has failed over to the untouched circuit before starting"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-09",
			Body: "1. Confirm [[globex-expressroute-circuit-v2]] (or its predecessor, " +
				"[[globex-expressroute-circuit-v1]], if still mid-migration) is healthy on both " +
				"circuits.\n" +
				"2. Fail traffic over to one circuit.\n" +
				"3. Request the bandwidth upgrade on the idle circuit.\n" +
				"4. Repeat for the other circuit once the first upgrade is confirmed.\n\n" +
				"This same one-circuit-at-a-time pattern is what a hub-wide capacity upgrade " +
				"would use if it becomes necessary; see " +
				"[[globex-storage-lifecycle-rules-not-running]] for an unrelated storage issue " +
				"raised during the same capacity review.\n",
		},
		{
			UID: uid(31), Slug: "globex-storage-lifecycle-rules-not-running", Type: "issue", Scope: "client-globex",
			Title: "Storage lifecycle rules are not actually deleting anything",
			Tags:  []string{"customer/globex", "layer/storage"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Three accounts under immutability lock have zero lifecycle-rule deletes in 90 days"},
				{ID: "c2", Status: "active", Text: "Storage cost for those accounts has grown eight percent month over month"},
			},
			Status: "open", Owner: "Sana Idris", LastVerified: "2026-09-06",
			Body: "Three storage accounts have had zero successful lifecycle-rule deletes in " +
				"the last 90 days despite showing the rule as applied, matching the block " +
				"described in [[globex-storage-account-immutability-lock-blocks-lifecycle]]. " +
				"Storage cost on those accounts is climbing as a result.\n",
		},
		{
			UID: uid(32), Slug: "globex-ingress-502-window-2026-07", Type: "incident", Scope: "client-globex",
			Title: "Globex ingress 502 window, July 2026",
			Tags:  []string{"customer/globex", "customer/globex/consumer", "layer/compute", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Consumer-facing ingress returned intermittent 502s for eighteen minutes"},
				{ID: "c2", Status: "active", Text: "Root cause traced to the cert renewal race, not a backend failure"},
				{ID: "c3", Status: "active", Text: "Circuit capacity was ruled out early; both ExpressRoute circuits were healthy"},
			},
			Status: "resolved", Owner: "Sana Idris", LastVerified: "2026-09-07",
			Body: "The consumer-facing ingress returned intermittent 502s for eighteen minutes. " +
				"Root cause was [[globex-aks-ingress-cert-renewal-race]], not a backend or " +
				"network problem — [[globex-expressroute-circuit-v2]] was confirmed healthy on " +
				"both circuits throughout. [[globex-move-to-managed-cert-renewal]] is the " +
				"follow-up fix.\n",
		},
	}
}
