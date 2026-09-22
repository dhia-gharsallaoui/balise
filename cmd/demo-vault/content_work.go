package main

// workPages is the "work" scope: the shared platform/infrastructure content every client
// scope below refers back to. It is intentionally the largest scope (16 pages), matching how
// a real vault accumulates far more shared platform knowledge than any single client's slice.
func workPages() []page {
	return []page{
		{
			UID: uid(1), Slug: "expressroute-transit", Type: "entity", Scope: "work",
			Title:   "ExpressRoute Transit",
			Aliases: []string{"er-transit"},
			Tags:    []string{"vendor/azure/expressroute", "layer/network"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Shared transit hub carrying all client circuits into the platform backbone"},
				{ID: "c2", Status: "active", Text: "Each client circuit peers here rather than to a dedicated gateway"},
				{ID: "c3", Status: "active", Text: "Capacity is tracked per circuit, not per gateway, since the hub is shared"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-04",
			Body: "The transit hub is the single ExpressRoute gateway pair every client circuit " +
				"terminates on. It exists so a new client onboarding never needs a new gateway " +
				"deployment, only a new circuit peering against the existing hub.\n\n" +
				"See [[expressroute-dual-circuit-standard]] for how redundancy is modelled on top " +
				"of this shared hub, and [[expressroute-capacity-headroom-shrinking]] for the " +
				"current capacity picture.\n",
		},
		{
			UID: uid(2), Slug: "shared-aks-fleet", Type: "entity", Scope: "work",
			Title:   "Shared AKS Fleet",
			Aliases: []string{"aks-fleet"},
			Tags:    []string{"vendor/azure/aks", "layer/compute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "One fleet manager rolls node image updates across every member cluster"},
				{ID: "c2", Status: "active", Text: "Member clusters opt in per subscription, not per node pool"},
				{ID: "c3", Status: "active", Text: "Fleet-wide rollout is staged over three rings, one week apart"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-08-11",
			Body: "The fleet groups every client-facing AKS cluster under one update manager so a " +
				"node image or control-plane bump ships once instead of per cluster.\n\n" +
				"Current and prior node images are tracked as state pages: " +
				"[[aks-fleet-node-image-2026-09]] is active, [[aks-fleet-node-image-2026-06]] is " +
				"the superseded prior baseline. [[standardize-on-aks-fleet-model]] records why " +
				"per-client clusters were retired in favour of this shared model.\n",
		},
		{
			UID: uid(3), Slug: "aks-fleet-node-image-2026-06", Type: "state", Scope: "work",
			Title:        "AKS fleet node image, June 2026 baseline",
			InstanceOf:   "shared-aks-fleet",
			SupersededBy: "aks-fleet-node-image-2026-09",
			Tags:         []string{"layer/compute", "vendor/azure/aks"},
			AsOf:         "2026-06-02",
			Claims: []claim{
				{ID: "c1", Status: "superseded", AsOf: "2026-09-01", Text: "Ubuntu 22.04 image with kernel 5.15, fleet-wide since June ring rollout"},
				{ID: "c2", Status: "superseded", AsOf: "2026-09-01", Text: "Contains the cgroup v1 default that later caused pod eviction flapping"},
			},
			Status: "superseded", Owner: "Marcus Webb", LastVerified: "2026-09-01",
			Body: "This was the fleet's node image baseline from the June ring rollout through " +
				"early September. It is superseded by " +
				"[[aks-fleet-node-image-2026-09]], which fixes the cgroup default noted below.\n",
		},
		{
			UID: uid(4), Slug: "aks-fleet-node-image-2026-09", Type: "state", Scope: "work",
			Title:      "AKS fleet node image, September 2026 baseline",
			InstanceOf: "shared-aks-fleet",
			About:      []string{"aks-node-pool-recreate-drops-taints"},
			Tags:       []string{"layer/compute", "vendor/azure/aks"},
			AsOf:       "2026-09-08",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Ubuntu 22.04 image with kernel 6.2, switched to cgroup v2 by default"},
				{ID: "c2", Status: "active", Text: "Rolled out fleet-wide over three weekly rings ending September 8"},
				{ID: "c3", Status: "active", Text: "Node pool recreate still drops custom taints; see linked gotcha"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-09-08",
			Body: "Current fleet baseline. Fixes the cgroup v1 pod eviction flapping from " +
				"[[aks-fleet-node-image-2026-06]], but does not change the node pool recreate " +
				"behaviour tracked in [[aks-node-pool-recreate-drops-taints]].\n",
		},
		{
			UID: uid(5), Slug: "aks-node-pool-recreate-drops-taints", Type: "gotcha", Scope: "work",
			Title:  "Recreating an AKS node pool drops custom taints",
			Vendor: "shared-aks-fleet",
			Tags:   []string{"layer/compute", "vendor/azure/aks"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Node pool recreate resets taints to the pool default, dropping custom ones"},
				{ID: "c2", Status: "active", Text: "Re-apply taints via the fleet update run, not the base pool manifest"},
				{ID: "c3", Status: "active", Text: "Affects every fleet member, confirmed across three separate ring rollouts"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-09-10",
			Body: "Any node pool recreate operation — including the fleet's own ring rollout — " +
				"resets that pool's taints to whatever the base pool manifest declares, silently " +
				"dropping any taint added by hand afterwards.\n\n" +
				"Workaround: re-add the taint through the fleet update run's post-step hook so it " +
				"survives the next recreate, rather than patching the live pool directly.\n",
		},
		{
			UID: uid(6), Slug: "terraform-subscription-foreach-reorder", Type: "gotcha", Scope: "work",
			Title: "Terraform for_each over subscriptions reorders on rename",
			Tags:  []string{"layer/iac"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Renaming a subscription alias reorders the for_each map, forcing unrelated recreates"},
				{ID: "c2", Status: "active", Text: "Keying the for_each on subscription ID instead of alias avoids the reorder"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-20",
			Body: "The landing zone module iterates subscriptions with `for_each` keyed on the " +
				"human-readable alias. Renaming one alias changes the map's key set, which " +
				"Terraform treats as every other key changing position, and it plans recreates " +
				"for resources that did not actually change.\n\n" +
				"Fix: key on subscription ID and keep the alias as a plain attribute.\n",
		},
		{
			UID: uid(7), Slug: "azfw-policy-import-priority-clamp", Type: "gotcha", Scope: "work",
			Title: "Azure Firewall policy import clamps rule priority to 65000",
			Tags:  []string{"layer/network", "layer/security", "vendor/azure/azfw"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Imported rule collections above priority 65000 are silently clamped on import"},
				{ID: "c2", Status: "active", Text: "Clamped rules still show their original priority in the portal view"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-07-30",
			Body: "Importing a firewall policy exported from a different tenant can carry rule " +
				"collection priorities above the 65000 ceiling. The import clamps them rather " +
				"than rejecting the import, and the portal keeps showing the original number, so " +
				"the clamp is easy to miss until rule ordering behaves unexpectedly.\n",
		},
		{
			UID: uid(8), Slug: "adopt-terraform-workspaces-per-subscription", Type: "decision", Scope: "work",
			Title: "Adopt one Terraform workspace per subscription",
			Tags:  []string{"layer/iac"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "One workspace per subscription replaces the prior single-workspace-per-region model"},
				{ID: "c2", Status: "active", Text: "State blast radius is now bounded to one subscription per apply"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-01",
			Body: "Adopted a one-workspace-per-subscription layout so a bad apply can only affect " +
				"one subscription's state. This replaced the earlier single-workspace-per-region " +
				"layout, which meant every apply touched every client sharing that region.\n\n" +
				"[[onboard-new-subscription-to-landing-zone]] now provisions the workspace as its " +
				"first step.\n",
		},
		{
			UID: uid(9), Slug: "standardize-on-aks-fleet-model", Type: "decision", Scope: "work",
			Title: "Standardize on the shared AKS fleet model",
			Tags:  []string{"layer/compute", "vendor/azure/aks"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Per-client AKS clusters are retired in favour of the shared fleet"},
				{ID: "c2", Status: "active", Text: "Fleet membership is the default for any new client cluster"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-08-15",
			Body: "Standardized on [[shared-aks-fleet]] as the only supported AKS operating model. " +
				"Existing per-client clusters are migrated in on their next upgrade window rather " +
				"than all at once.\n",
		},
		{
			UID: uid(10), Slug: "expressroute-dual-circuit-standard", Type: "decision", Scope: "work",
			Title: "Require dual ExpressRoute circuits for any client above tier 2",
			Tags:  []string{"layer/network", "vendor/azure/expressroute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Tier 2+ clients get two circuits in different peering locations"},
				{ID: "c2", Status: "active", Text: "Tier 1 clients keep a single circuit unless they request otherwise"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-05",
			Body: "Any client above tier 2 now gets two [[expressroute-transit]] circuits in " +
				"different peering locations rather than one. [[expressroute-capacity-headroom-shrinking]] " +
				"is tracking how much headroom this standard leaves on the shared hub.\n",
		},
		{
			UID: uid(11), Slug: "onboard-new-subscription-to-landing-zone", Type: "procedure", Scope: "work",
			Title: "Onboard a new subscription to the landing zone",
			Tags:  []string{"layer/iac", "layer/identity"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Create the subscription-scoped Terraform workspace before any resource apply"},
				{ID: "c2", Status: "active", Text: "Grant the landing zone service principal Owner only at the subscription scope"},
				{ID: "c3", Status: "active", Text: "Run drift detection once immediately after the first apply completes"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-18",
			Body: "1. Create the subscription and its dedicated Terraform workspace per " +
				"[[adopt-terraform-workspaces-per-subscription]].\n" +
				"2. Grant the landing zone service principal Owner at the subscription scope " +
				"only — never at management group scope.\n" +
				"3. Apply the baseline landing zone module.\n" +
				"4. Run drift detection once immediately after; see " +
				"[[drift-detector-false-positives-on-tags]] for a known false-positive pattern on " +
				"the first run.\n",
		},
		{
			UID: uid(12), Slug: "rotate-aks-fleet-cluster-credentials", Type: "procedure", Scope: "work",
			Title: "Rotate shared AKS fleet cluster credentials",
			Tags:  []string{"layer/compute", "layer/identity", "vendor/azure/aks"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Rotate the fleet manager's credential first, member clusters second"},
				{ID: "c2", Status: "active", Text: "Old credentials remain valid for one hour after rotation to avoid downtime"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-09-05",
			Body: "1. Rotate [[shared-aks-fleet]]'s manager credential.\n" +
				"2. Rotate each member cluster's credential, oldest cluster first.\n" +
				"3. Confirm the fleet manager can still reach every member before revoking the " +
				"prior credential.\n",
		},
		{
			UID: uid(13), Slug: "drift-detector-false-positives-on-tags", Type: "issue", Scope: "work",
			Title: "Drift detector reports false positives on cost-center tags",
			Tags:  []string{"layer/iac", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Cost-center tags applied by the billing sync are flagged as drift"},
				{ID: "c2", Status: "active", Text: "Detector runs hourly and re-flags the same tags every cycle"},
			},
			Status: "open", Owner: "Ada Okonkwo", LastVerified: "2026-09-12",
			Body: "The drift detector treats any tag it did not itself apply as drift, " +
				"including cost-center tags written by the separate billing sync job. This makes " +
				"every subscription look perpetually drifted on that one field.\n\n" +
				"Planned fix: exclude the billing sync's tag keys from the drift comparison.\n",
		},
		{
			UID: uid(14), Slug: "expressroute-capacity-headroom-shrinking", Type: "issue", Scope: "work",
			Title: "ExpressRoute transit hub capacity headroom is shrinking",
			Tags:  []string{"layer/network", "vendor/azure/expressroute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Aggregate circuit usage crossed 70 percent of hub capacity in August"},
				{ID: "c2", Status: "active", Text: "Each new tier 2+ client adds two circuits under the dual standard"},
			},
			Status: "open", Owner: "Ada Okonkwo", LastVerified: "2026-09-02",
			Body: "Aggregate usage across all circuits on [[expressroute-transit]] crossed 70 " +
				"percent of the hub's provisioned capacity in August, driven partly by " +
				"[[expressroute-dual-circuit-standard]] doubling circuit count for new tier 2+ " +
				"clients. A capacity upgrade is being scoped; Globex's own circuit upgrade " +
				"procedure follows the pattern this hub-wide upgrade is expected to use.\n",
		},
		{
			UID: uid(15), Slug: "landing-zone-pipeline-outage-2026-08", Type: "incident", Scope: "work",
			Title: "Landing zone pipeline outage, August 2026",
			Tags:  []string{"layer/iac", "layer/observability", "layer/agents"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Pipeline queue backed up for six hours after a shared runner pool outage"},
				{ID: "c2", Status: "active", Text: "An on-call triage agent drained the queue once the runner pool recovered"},
				{ID: "c3", Status: "active", Text: "No subscription apply was lost; queued runs replayed in original order"},
			},
			Status: "resolved", Owner: "Ada Okonkwo", LastVerified: "2026-08-22",
			Body: "The shared CI runner pool backing every landing zone pipeline run went " +
				"unhealthy for roughly six hours, backing up the apply queue across every " +
				"subscription onboarded via [[onboard-new-subscription-to-landing-zone]].\n\n" +
				"Once the runner pool recovered, the on-call triage agent drained the backlog in " +
				"original order; no apply was lost. This also touched capacity planning — see " +
				"[[expressroute-capacity-headroom-shrinking]] for the unrelated but concurrently " +
				"tracked hub capacity issue raised during the same on-call window.\n",
		},
		{
			UID: uid(16), Slug: "platform-oncall-escalation-contacts", Type: "note", Scope: "work",
			Title: "Platform on-call escalation contacts",
			Tags:  []string{"layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Primary on-call rotates weekly, Monday 09:00 handoff"},
				{ID: "c2", Status: "active", Text: "Secondary escalation is the platform lead, paged after fifteen minutes"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-09-15",
			Body: "Primary on-call rotates weekly with a Monday 09:00 handoff. Unacknowledged " +
				"pages escalate to the secondary (platform lead) after fifteen minutes.\n\n" +
				"For the step-by-step escalation flowchart, see " +
				"[[incident-response-master-runbook]] — that page has not been written yet; " +
				"this reference is left in deliberately as a real, unfixed dangling link.\n",
		},
	}
}
