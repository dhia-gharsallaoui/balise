package main

// globexCrossScopeDanglingComment documents a deliberate, permanent gotcha in the generated
// vault: the body below links to [[shared-event-bus]], the work-scope page this replicated
// pipeline is really built on top of, but wikilink resolution never crosses scope boundaries
// -- only slug lookup within the same scope -- so the link is left dangling on purpose rather
// than quietly rewritten to something that would resolve but mean something else.
const globexCrossScopeDanglingComment = "" +
	"# The body's [[shared-event-bus]] wikilink names work/shared-event-bus, the platform-wide\n" +
	"# Kafka cluster this replicated pipeline actually runs on top of -- but link resolution\n" +
	"# never crosses scope boundaries, only slug lookup within this same scope, so the link is\n" +
	"# left dangling on purpose rather than quietly rewritten to something that would resolve\n" +
	"# but mean something else.\n"

// globexPages is the "client-globex" scope: Globex's order event pipeline and SSO knowledge.
func globexPages() []page {
	return []page{
		{
			UID: uid(25), Slug: "globex-order-events-pipeline-single-broker", Type: "state", Scope: "client-globex",
			Title:        "Globex order events pipeline, single-broker baseline",
			SupersededBy: "globex-order-events-pipeline-replicated",
			Tags:         []string{"customer/globex", "layer/queue", "vendor/kafka"},
			AsOf:         "2026-07-01",
			Claims: []claim{
				{ID: "c1", Status: "superseded", AsOf: "2026-08-20", Text: "Order events published to a single, unreplicated Kafka broker dedicated to Globex"},
				{ID: "c2", Status: "superseded", AsOf: "2026-08-20", Text: "A broker restart during a deploy caused a visible gap in order event delivery"},
			},
			Status: "superseded", Owner: "Sana Idris", LastVerified: "2026-08-20",
			Body: "The original Globex order events pipeline ran on a single, unreplicated Kafka broker. Superseded by [[globex-order-events-pipeline-replicated]] after a broker restart caused a visible delivery gap.\n",
		},
		{
			Comment: globexCrossScopeDanglingComment,
			UID:     uid(26), Slug: "globex-order-events-pipeline-replicated", Type: "state", Scope: "client-globex",
			Title:   "Globex order events pipeline, replicated",
			Aliases: []string{"globex-order-events-v2"},
			Tags:    []string{"customer/globex", "layer/queue", "vendor/kafka"},
			AsOf:    "2026-08-20",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Order events now publish to a three-broker replicated Kafka cluster dedicated to Globex"},
				{ID: "c2", Status: "active", Text: "The cluster runs on top of the same shared event bus platform every other tenant uses"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-10",
			Body: "Current baseline: a three-broker replicated cluster, migrated off [[globex-order-events-pipeline-single-broker]]. It runs on top of [[shared-event-bus]], the same platform-wide Kafka deployment every tenant shares.\n",
		},
		{
			UID: uid(27), Slug: "globex-sso-nested-group-claim-mapping-drops-groups", Type: "gotcha", Scope: "client-globex",
			Title: "Okta SSO nested group claim mapping drops nested groups",
			Tags:  []string{"customer/globex", "layer/identity", "layer/security", "vendor/okta"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Okta's group claim mapping only enumerates a user's direct group memberships"},
				{ID: "c2", Status: "active", Text: "A user who only belongs to a nested subgroup gets no group claim at all on login"},
				{ID: "c3", Status: "active", Text: "Manually flattening the nested groups into direct memberships is the only confirmed workaround today"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-11",
			Body: "Okta's group claim mapping enumerates only a user's direct group memberships, not memberships inherited through a nested subgroup. A user who only belongs to a nested subgroup receives no group claim at login at all, which [[globex-sso-nested-group-users-missing-access]] tracks the access-loss impact of.\n",
		},
		{
			UID: uid(28), Slug: "globex-api-gateway-cert-renewal-races-health-check", Type: "gotcha", Scope: "client-globex",
			Title: "API gateway certificate renewal races the health check",
			Tags:  []string{"customer/globex/consumer", "layer/backend", "layer/security", "vendor/nginx"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The nginx-based gateway reloads its TLS certificate on a fixed nightly timer"},
				{ID: "c2", Status: "active", Text: "The load balancer's health check can hit the gateway mid-reload and see a stale certificate for a few seconds"},
				{ID: "c3", Status: "active", Text: "A health check failure during that window pulls a healthy gateway instance out of rotation"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-01",
			Body: "The nginx-based API gateway reloads its TLS certificate on a fixed nightly timer. The load balancer's health check can catch the gateway mid-reload and briefly see a stale certificate, pulling an otherwise healthy instance out of rotation. [[globex-api-gateway-502-window-2026-07]] is the incident this produced.\n",
		},
		{
			UID: uid(29), Slug: "globex-move-to-managed-cert-renewal", Type: "decision", Scope: "client-globex",
			Title: "Move API gateway certificate renewal to a managed rotation service",
			Tags:  []string{"customer/globex", "layer/backend", "layer/security"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Certificate renewal moves off the nightly cron timer onto a managed rotation service with graceful reload"},
				{ID: "c2", Status: "active", Text: "The managed service drains connections before swapping the certificate instead of reloading in place"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-02",
			Body: "Decided to replace the nightly cron-based renewal behind [[globex-api-gateway-cert-renewal-races-health-check]] with a managed rotation service that drains connections before swapping the certificate.\n",
		},
		{
			UID: uid(30), Slug: "globex-kafka-broker-capacity-upgrade", Type: "procedure", Scope: "client-globex",
			Title: "Upgrade Globex's Kafka broker capacity",
			Tags:  []string{"customer/globex", "layer/queue", "vendor/kafka"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A capacity upgrade adds one broker at a time and waits for full replication before adding the next"},
				{ID: "c2", Status: "active", Text: "Partition reassignment runs during Globex's lowest-traffic window, never during business hours"},
			},
			Status: "active", Owner: "Sana Idris", LastVerified: "2026-09-05",
			Body: "1. Add one broker to [[globex-order-events-pipeline-replicated]] at a time.\n2. Wait for full replication before adding the next.\n3. Run partition reassignment only during Globex's lowest-traffic window.\n",
		},
		{
			UID: uid(31), Slug: "globex-sso-nested-group-users-missing-access", Type: "issue", Scope: "client-globex",
			Title: "Users in nested Okta groups are missing expected access",
			Tags:  []string{"customer/globex", "layer/identity", "layer/security"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Several Globex users report missing access that their nested group membership should grant"},
				{ID: "c2", Status: "active", Text: "Support currently resolves each case manually by adding the user to a direct group as a workaround"},
			},
			Status: "open", Owner: "Sana Idris", LastVerified: "2026-09-11",
			Body: "Direct fallout of [[globex-sso-nested-group-claim-mapping-drops-groups]]: users in nested-only groups are missing access their membership should grant. Each case is currently resolved manually.\n",
		},
		{
			UID: uid(32), Slug: "globex-api-gateway-502-window-2026-07", Type: "incident", Scope: "client-globex",
			Title: "API gateway 502 window, July 2026",
			Tags:  []string{"customer/globex/consumer", "layer/backend", "layer/security", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A four-minute window of intermittent 502 responses followed the nightly certificate reload"},
				{ID: "c2", Status: "active", Text: "Health check failures during the reload pulled two of four gateway instances out of rotation at once"},
				{ID: "c3", Status: "active", Text: "The remaining two instances could not absorb full traffic, producing visible 502s for real requests"},
			},
			Status: "resolved", Owner: "Sana Idris", LastVerified: "2026-08-01",
			Body: "A four-minute window of intermittent 502s followed the nightly certificate reload described in [[globex-api-gateway-cert-renewal-races-health-check]]. Health check failures pulled two of four instances out of rotation at once, and the remaining two could not absorb full traffic.\n",
		},
	}
}
