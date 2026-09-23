package main

// workPages is the "work" scope: the shared platform/infrastructure content every client
// scope below refers back to. It is intentionally the largest scope (16 pages), matching how
// a real vault accumulates far more shared platform knowledge than any single client's slice.
func workPages() []page {
	return []page{
		{
			UID: uid(1), Slug: "shared-event-bus", Type: "note", Scope: "work",
			Title:   "Shared Event Bus",
			Aliases: []string{"event-bus"},
			Tags:    []string{"layer/queue", "vendor/kafka"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Every service publishes domain events onto one shared Kafka cluster instead of running its own broker"},
				{ID: "c2", Status: "active", Text: "Topics are namespaced by service name so two teams can never collide on the same topic"},
				{ID: "c3", Status: "active", Text: "Consumer groups rebalance automatically as a service scales its pod count up or down"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-04",
			Body: "The shared cluster is the single Kafka deployment every service publishes domain events onto, rather than each service running its own broker.\n\nSee [[event-bus-consumer-lag-headroom-shrinking]] for the current capacity picture, and [[cicd-pipeline-outage-2026-08]] for an incident that briefly touched this cluster's own deploy pipeline.\n",
		},
		{
			UID: uid(2), Slug: "shared-postgres-cluster", Type: "note", Scope: "work",
			Title:   "Shared Postgres Cluster",
			Aliases: []string{"postgres-cluster"},
			Tags:    []string{"layer/database", "vendor/postgres"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "One Postgres cluster hosts every service's schema instead of a database per service"},
				{ID: "c2", Status: "active", Text: "Each service connects through a role scoped to only its own schema"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-08-11",
			Body: "The shared cluster hosts one schema per service rather than a dedicated instance per service. Current and prior major-version baselines are tracked as state pages: [[postgres-cluster-version-v16]] is active, [[postgres-cluster-version-v14]] is the superseded prior baseline. [[standardize-on-shared-postgres-cluster]] records why per-service instances were retired in favor of this shared model.\n",
		},
		{
			UID: uid(3), Slug: "postgres-cluster-version-v14", Type: "state", Scope: "work",
			Title:        "Shared Postgres cluster version, v14 baseline",
			SupersededBy: "postgres-cluster-version-v16",
			Tags:         []string{"layer/database", "vendor/postgres"},
			AsOf:         "2026-06-02",
			Claims: []claim{
				{ID: "c1", Status: "superseded", AsOf: "2026-08-15", Text: "Cluster ran Postgres 14 from launch through mid-August 2026"},
				{ID: "c2", Status: "superseded", AsOf: "2026-08-15", Text: "Statistics targets on the largest tables were hand-tuned above the v14 default"},
			},
			Status: "superseded", Owner: "Marcus Webb", LastVerified: "2026-08-15",
			Body: "This was the [[shared-postgres-cluster]]'s version baseline from launch through mid-August. It is superseded by [[postgres-cluster-version-v16]], which does not preserve the hand-tuned statistics targets noted below — see [[postgres-major-upgrade-drops-statistics-targets]].\n",
		},
		{
			UID: uid(4), Slug: "postgres-cluster-version-v16", Type: "state", Scope: "work",
			Title: "Shared Postgres cluster version, v16 baseline",
			About: []string{"postgres-major-upgrade-drops-statistics-targets"},
			Tags:  []string{"layer/database", "vendor/postgres"},
			AsOf:  "2026-08-15",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Cluster moved to Postgres 16 in August 2026 for its native logical replication improvements"},
				{ID: "c2", Status: "active", Text: "Hand-tuned statistics targets have to be reapplied after every major version upgrade"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-09-01",
			Body: "Current baseline for the [[shared-postgres-cluster]]. The upgrade did not preserve custom statistics targets, tracked in [[postgres-major-upgrade-drops-statistics-targets]].\n",
		},
		{
			UID: uid(5), Slug: "postgres-major-upgrade-drops-statistics-targets", Type: "gotcha", Scope: "work",
			Title: "Postgres major version upgrade drops custom statistics targets",
			Tags:  []string{"layer/database", "vendor/postgres"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "pg_upgrade recreates each table's statistics configuration at the server default"},
				{ID: "c2", Status: "active", Text: "Custom ALTER TABLE ... SET STATISTICS overrides silently revert to 100"},
				{ID: "c3", Status: "active", Text: "Query plans degrade within a day as the planner's row estimates drift from reality"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-09-02",
			Body: "Any pg_upgrade run against the [[shared-postgres-cluster]] resets every table's statistics target to the server default, silently dropping hand-tuned overrides.\n\nWorkaround: re-apply the custom statistics targets from the tracked list immediately after upgrade, before ANALYZE runs against the new default.\n",
		},
		{
			UID: uid(6), Slug: "migration-rename-changes-apply-order", Type: "gotcha", Scope: "work",
			Title: "Renaming a migration file changes its apply order",
			Tags:  []string{"layer/database", "layer/ci"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The migration tool orders files by their numeric timestamp prefix, not by git history"},
				{ID: "c2", Status: "active", Text: "Renaming a migration file to fix a typo changes that prefix and its position in the run order"},
				{ID: "c3", Status: "active", Text: "A later migration can silently apply before the dependency it assumed already ran"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-20",
			Body: "The migration runner orders files purely by their numeric timestamp prefix. Renaming a file — even just to fix a typo in its description — changes that prefix and can reorder it relative to migrations that depend on it running first.\n\nFix: never rename an already-applied migration file; add a new one instead.\n",
		},
		{
			UID: uid(7), Slug: "redis-eviction-removes-keys-before-ttl-expires", Type: "gotcha", Scope: "work",
			Title: "Redis eviction removes keys before their TTL expires",
			Tags:  []string{"layer/cache", "vendor/redis"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "allkeys-lru eviction runs whenever the cluster is near maxmemory, independent of any key's TTL"},
				{ID: "c2", Status: "active", Text: "A session key with an hour left on its TTL can still be evicted first if it is the least recently used"},
				{ID: "c3", Status: "active", Text: "Code that assumes TTL is the only way a cached value disappears breaks on the first null it doesn't check for"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-09-05",
			Body: "The shared cache runs allkeys-lru eviction, which can remove a key long before its TTL would have expired if the cluster is near maxmemory. Any read path that assumes a present key means an unexpired one will fail exactly when the cache is under the most pressure.\n",
		},
		{
			UID: uid(8), Slug: "adopt-schema-per-service", Type: "decision", Scope: "work",
			Title: "Adopt one Postgres schema per service",
			Tags:  []string{"layer/database", "layer/backend"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Every service gets its own schema in the shared cluster instead of a shared public schema"},
				{ID: "c2", Status: "active", Text: "A bad migration in one schema cannot lock or corrupt another service's tables"},
				{ID: "c3", Status: "active", Text: "Cross-schema joins are discouraged so services stay independently deployable"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-08-10",
			Body: "Adopted one schema per service on the [[shared-postgres-cluster]] instead of a shared public schema, so a bad migration is contained to its own service. [[onboard-service-to-shared-postgres]] provisions the schema as its first step.\n",
		},
		{
			UID: uid(9), Slug: "standardize-on-shared-postgres-cluster", Type: "decision", Scope: "work",
			Title: "Standardize on the shared Postgres cluster model",
			Tags:  []string{"layer/database", "layer/infra"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "New services default onto the shared cluster instead of provisioning a dedicated instance"},
				{ID: "c2", Status: "active", Text: "A dedicated instance is still available by exception for workloads with unusual isolation needs"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-08-12",
			Body: "Standardized on [[shared-postgres-cluster]] as the default for any new service. A dedicated instance remains available by exception, granted rarely and only for workloads with genuinely unusual isolation requirements.\n",
		},
		{
			UID: uid(10), Slug: "require-read-replica-above-tier-2", Type: "decision", Scope: "work",
			Title: "Require an async read replica above tier 2 read load",
			Tags:  []string{"layer/database", "layer/infra"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Any service whose read traffic crosses the tier 2 threshold must add an async replica"},
				{ID: "c2", Status: "active", Text: "Read replicas absorb reporting and analytics queries so they cannot starve primary writes"},
				{ID: "c3", Status: "active", Text: "The replica lag budget is documented per service, not assumed to be zero"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-08-14",
			Body: "Any service crossing the tier 2 read-load threshold on [[shared-postgres-cluster]] must add an async read replica, so reporting and analytics queries can no longer starve primary writes.\n",
		},
		{
			UID: uid(11), Slug: "onboard-service-to-shared-postgres", Type: "procedure", Scope: "work",
			Title: "Onboard a new service to the shared Postgres cluster",
			Tags:  []string{"layer/database", "layer/backend"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A new service requests a schema and a scoped role before its first migration runs"},
				{ID: "c2", Status: "active", Text: "The onboarding checklist requires a connection pool limit before granting production access"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-08-18",
			Body: "1. Request a schema and a role scoped to it, per [[adopt-schema-per-service]].\n2. Set a connection pool limit before requesting production access.\n3. Run the first migration and confirm it only touched the new schema.\n",
		},
		{
			UID: uid(12), Slug: "rotate-shared-postgres-credentials", Type: "procedure", Scope: "work",
			Title: "Rotate shared Postgres cluster credentials",
			Tags:  []string{"layer/database", "layer/security"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Credentials rotate on a fixed quarterly schedule, not only after a suspected leak"},
				{ID: "c2", Status: "active", Text: "Every service reads its password from the secrets manager, never from a checked-in config file"},
				{ID: "c3", Status: "active", Text: "The old credential stays valid for one hour after rotation so in-flight connections drain cleanly"},
			},
			Status: "active", Owner: "Marcus Webb", LastVerified: "2026-09-05",
			Body: "1. Rotate each service's role credential in the secrets manager, never in a checked-in config file.\n2. Leave the old credential valid for one hour so in-flight connections drain cleanly.\n3. Confirm every service has picked up the new credential before revoking the old one.\n\nThis runs quarterly on schedule against [[shared-postgres-cluster]], not only after a suspected leak.\n",
		},
		{
			UID: uid(13), Slug: "ci-flakiness-detector-false-positives-on-retries", Type: "issue", Scope: "work",
			Title: "CI flakiness detector reports false positives on retried jobs",
			Tags:  []string{"layer/ci", "vendor/github_actions"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The detector flags any job whose second attempt passed as a flaky test"},
				{ID: "c2", Status: "active", Text: "A job retried after a runner network blip gets the same flaky label as a genuinely nondeterministic test"},
			},
			Status: "open", Owner: "Ada Okonkwo", LastVerified: "2026-09-12",
			Body: "The flakiness detector treats any job that failed once and passed on retry as flaky, with no distinction between a genuinely nondeterministic test and a job that failed because the runner briefly lost network.\n\nPlanned fix: only count a retry as flaky evidence if the first failure's error signature is test-side, not infrastructure-side.\n",
		},
		{
			UID: uid(14), Slug: "event-bus-consumer-lag-headroom-shrinking", Type: "issue", Scope: "work",
			Title: "Shared event bus consumer lag headroom is shrinking",
			Tags:  []string{"layer/queue", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Average consumer lag across the shared cluster has grown every month since March"},
				{ID: "c2", Status: "active", Text: "Two more services are scheduled to onboard before the next capacity review"},
			},
			Status: "open", Owner: "Ada Okonkwo", LastVerified: "2026-09-10",
			Body: "Aggregate consumer lag on [[shared-event-bus]] has grown every month since March, and two more services are scheduled to onboard before the next capacity review. A broker capacity upgrade is being scoped.\n",
		},
		{
			UID: uid(15), Slug: "cicd-pipeline-outage-2026-08", Type: "incident", Scope: "work",
			Title: "CI/CD pipeline outage, August 2026",
			Tags:  []string{"layer/ci", "layer/release", "vendor/github_actions"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The shared GitHub Actions runner pool ran out of capacity and queued every deploy"},
				{ID: "c2", Status: "active", Text: "On-call drained the backlog by cancelling non-essential scheduled jobs first"},
				{ID: "c3", Status: "active", Text: "No deploy was lost; queued runs replayed in original order once capacity recovered"},
			},
			Status: "resolved", Owner: "Ada Okonkwo", LastVerified: "2026-08-22",
			Body: "The shared GitHub Actions runner pool backing every deploy pipeline ran out of capacity for roughly six hours, queuing every pending deploy across every service.\n\nOn-call drained the backlog by cancelling non-essential scheduled jobs first, then let queued deploys replay in original order once capacity recovered. No deploy was lost.\n",
		},
		{
			UID: uid(16), Slug: "platform-oncall-escalation-contacts", Type: "note", Scope: "work",
			Title: "Platform on-call escalation contacts",
			Tags:  []string{"layer/observability", "layer/agents", "vendor/pagerduty"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Primary on-call carries the pager for one week at a time, handoff every Monday"},
				{ID: "c2", Status: "active", Text: "Escalating past secondary on-call pages the engineering director directly"},
			},
			Status: "active", Owner: "Ada Okonkwo", LastVerified: "2026-09-15",
			Body: "Primary on-call rotates weekly with a Monday 09:00 handoff. Unacknowledged pages escalate to secondary on-call, and past that, directly to the engineering director.\n\nFor the step-by-step escalation flowchart, see [[incident-response-master-runbook]] — that page has not been written yet; this reference is left in deliberately as a real, unfixed dangling link.\n",
		},
	}
}
