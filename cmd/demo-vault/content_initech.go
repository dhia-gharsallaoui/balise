package main

import (
	"fmt"
	"strings"
)

// oversizeTopic is one paragraph of the deliberately oversize onboarding note below; the
// vault needs at least one page well past the others' length to exercise chunking and
// pagination in anything reading claims or rendered bodies at scale.
type oversizeTopic struct {
	heading, topic, reason, extra string
}

var initechOnboardingTopics = []oversizeTopic{
	{"Worker fleet sizing", "the shared RabbitMQ worker fleet", "is sized for steady-state batch volume, not headline peaks", "a burst above that baseline queues rather than fails outright"},
	{"Email sending domain provisioning", "Initech's transactional email", "sends through a dedicated SendGrid subaccount rather than a shared one", "this keeps Initech's sender reputation isolated from every other tenant"},
	{"Backup retention", "database backups", "are retained for thirty days on a rolling window", "anything older is only available by restoring the prior month's archive tier"},
	{"API key rotation", "third-party API keys", "rotate on the same quarterly schedule as database credentials", "a key nearing expiry triggers a warning two weeks out, not a hard cutover"},
	{"Worker image updates", "the worker container image", "rebuilds nightly from the base image regardless of whether application code changed", "this is what actually keeps the base layer's security patches current"},
	{"Queue consumer scaling", "consumer count for the shared worker fleet", "scales on queue depth, not on CPU usage", "a CPU-bound job type can still look idle to the autoscaler while genuinely falling behind"},
	{"Email suppression lists", "SendGrid's suppression list", "silently drops sends to any address that previously bounced or complained", "a resend to a suppressed address returns success from the API but never actually leaves"},
	{"RabbitMQ permission grants", "queue permissions", "are granted per virtual host, not per queue", "a service asking for one queue's access gets read/write on every queue in that vhost"},
	{"Tag governance", "resource tags", "are enforced at provisioning time, not retrofitted after the fact", "anything provisioned outside the standard procedure is missing tags nobody notices until a cost review"},
	{"Monitoring alert rules", "alert thresholds for the worker fleet", "were set from the first month of production traffic", "they have not been revisited since traffic patterns shifted"},
	{"Capacity planning", "next quarter's fleet capacity", "is planned from the prior quarter's ninety-fifth percentile load", "not from the average, which would understate real headroom needs"},
	{"Batch job scheduling", "large batch jobs", "are scheduled to start after the lowest-traffic hour begins", "a job that starts late overlaps the fleet's peak and competes with live traffic"},
	{"Credential rotation", "the shared worker fleet's service credentials", "rotate through the same secrets manager every other tenant uses", "the rotation window is the same one-hour drain period documented for Postgres"},
	{"Worker container image updates", "a failed image build", "blocks that night's rebuild but does not roll back the previous image", "workers keep running the last good image until the next successful build"},
	{"Quota increase requests", "a queue or storage quota increase", "requires a written justification tied to a specific upcoming launch", "an open-ended request without a launch date is routinely declined"},
	{"Cost allocation tags", "the customer cost-allocation tag", "is required on every resource before it leaves the provisioning procedure", "a resource missing this tag shows up as unallocated spend at month end"},
	{"Snapshot retention", "worker fleet configuration snapshots", "are kept for ninety days beyond backup's thirty", "these cover configuration drift, not data, and are pruned independently"},
	{"Email subaccount provisioning", "a new SendGrid subaccount", "requires its own verified sending domain before its first send", "skipping verification queues the send indefinitely instead of failing it outright"},
	{"Worker restart timeout handling", "a worker that does not acknowledge a graceful restart within thirty seconds", "is force-killed and its in-flight job requeued", "a job that is not idempotent can be processed twice as a result"},
	{"Batch backlog escalation", "a batch backlog past the alert threshold for more than an hour", "pages on-call directly instead of waiting for the next business day", "this is what actually caught the June backlog before it grew further"},
}

func oversizeBody() string {
	var b strings.Builder
	b.WriteString("This note collects everything a new engineer needs before they touch the " +
		"Initech environment for the first time. It intentionally covers more ground than a " +
		"single focused page would, because the alternative -- splitting it into twenty tiny " +
		"pages nobody reads end to end during onboarding -- has been tried before and made the " +
		"gaps in a new hire's mental model worse, not better. Read it in one sitting.\n")
	for _, t := range initechOnboardingTopics {
		fmt.Fprintf(&b, "\n## %s\n\n", t.heading)
		fmt.Fprintf(&b, "On the Initech account, %s %s. In practice this means new engineers "+
			"should assume %s is not automatic and should confirm it directly rather than "+
			"guess, since %s.\n", t.topic, t.reason, t.topic, t.extra)
	}
	return b.String()
}

// initechPages is the "client-initech" scope: Initech's worker fleet, email delivery, and
// frontend build knowledge.
func initechPages() []page {
	return []page{
		{
			UID: uid(33), Slug: "initech-shared-worker-fleet", Type: "state", Scope: "client-initech",
			Title: "Initech shared worker fleet",
			Tags:  []string{"customer/initech", "layer/queue", "layer/backend", "vendor/rabbitmq", "vendor/docker"},
			AsOf:  "2026-07-15",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "One RabbitMQ-backed worker fleet processes every Initech background job type"},
				{ID: "c2", Status: "active", Text: "Workers run as Docker containers on a fixed baseline instance size"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-09-01",
			Body: "The shared fleet runs every Initech background job type on a fixed baseline Docker instance size. [[initech-standardize-burstable-worker-fleet]] records the decision to move this baseline onto burstable instances.\n",
		},
		{
			UID: uid(34), Slug: "initech-barrel-import-defeats-tree-shaking", Type: "gotcha", Scope: "client-initech",
			Title: "A barrel import defeats tree-shaking in the admin dashboard bundle",
			Tags:  []string{"customer/initech", "layer/frontend"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The admin dashboard imports its whole UI kit through one barrel index file"},
				{ID: "c2", Status: "active", Text: "The bundler cannot prove any single export from a barrel file is unused, so it keeps the entire kit"},
				{ID: "c3", Status: "active", Text: "Switching one page to import only the three components it actually uses cut that page's bundle by more than half"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-09-13",
			Body: "The admin dashboard pulls its whole UI kit through a single barrel index file. A bundler cannot prove any individual export from a barrel file is dead code, so it keeps the entire kit in every page that imports from it, no matter how few components that page actually renders.\n\nSwitching one heavy page to import only its three actually-used components directly cut that page's JS bundle by more than half. This has not yet been rolled out across the rest of the dashboard.\n",
		},
		{
			UID: uid(35), Slug: "initech-standardize-burstable-worker-fleet", Type: "decision", Scope: "client-initech",
			Title: "Standardize the worker fleet on burstable instances",
			Tags:  []string{"customer/initech", "layer/queue", "layer/infra"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The worker fleet baseline moves from fixed-size instances to burstable ones"},
				{ID: "c2", Status: "active", Text: "Burstable instances accumulate CPU credit during idle periods to cover short processing bursts"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-08-28",
			Body: "Standardized [[initech-shared-worker-fleet]] on burstable instances so idle-period CPU credit covers short processing bursts instead of over-provisioning a larger fixed baseline. [[initech-worker-fleet-cpu-credit-exhaustion]] tracks where this has not fully held up.\n",
		},
		{
			UID: uid(36), Slug: "initech-consolidate-email-into-shared-sendgrid", Type: "decision", Scope: "client-initech",
			Title: "Consolidate Initech's transactional email onto a shared SendGrid account",
			Tags:  []string{"customer/initech", "layer/email", "vendor/sendgrid"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Every Initech service sends transactional email through one shared SendGrid account instead of its own"},
				{ID: "c2", Status: "active", Text: "Each service gets its own subaccount and verified sending domain within the shared account"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-08-10",
			Body: "Consolidated onto one shared SendGrid account with per-service subaccounts and verified sending domains, so no service has to manage its own provider relationship. [[initech-provision-app-email-under-shared-account]] provisions a new service's subaccount.\n",
		},
		{
			UID: uid(37), Slug: "initech-provision-app-email-under-shared-account", Type: "procedure", Scope: "client-initech",
			Title: "Provision a new app's email under the shared SendGrid account",
			Tags:  []string{"customer/initech", "layer/email", "vendor/sendgrid"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A new service requests a subaccount and a verified sending domain before its first send"},
				{ID: "c2", Status: "active", Text: "Domain verification must complete before the service's first send is attempted, not in parallel with it"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-08-12",
			Body: "1. Request a subaccount under [[initech-consolidate-email-into-shared-sendgrid]].\n2. Verify the sending domain and wait for confirmation before attempting any send.\n3. Confirm the suppression list behavior with a test send to a known-good address.\n",
		},
		{
			UID: uid(38), Slug: "initech-worker-fleet-cpu-credit-exhaustion", Type: "issue", Scope: "client-initech",
			Title: "Worker fleet burns through CPU credit during sustained batch runs",
			Tags:  []string{"customer/initech", "layer/queue", "layer/infra"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A sustained batch run longer than twenty minutes exhausts a burstable worker's accumulated CPU credit"},
				{ID: "c2", Status: "active", Text: "Once credit is exhausted, the instance throttles to its baseline performance mid-job"},
			},
			Status: "open", Owner: "Leo Ferreira", LastVerified: "2026-09-12",
			Body: "Sustained batch runs longer than twenty minutes exhaust a burstable instance's accumulated CPU credit under [[initech-standardize-burstable-worker-fleet]], throttling performance mid-job rather than at a predictable boundary.\n",
		},
		{
			UID: uid(39), Slug: "initech-worker-backlog-2026-06", Type: "incident", Scope: "client-initech",
			Title: "Initech worker backlog, June 2026",
			Tags:  []string{"customer/initech", "layer/queue"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "A batch backlog crossed the alert threshold and paged on-call directly rather than waiting for business hours"},
				{ID: "c2", Status: "active", Text: "Root cause was CPU credit exhaustion on the worker fleet during an unusually long batch run"},
				{ID: "c3", Status: "active", Text: "The backlog drained fully within two hours of the page"},
			},
			Status: "resolved", Owner: "Leo Ferreira", LastVerified: "2026-06-20",
			Body: "The backlog escalation behavior described in the onboarding notes caught this: a batch backlog crossed the alert threshold and paged on-call directly. Root cause was [[initech-worker-fleet-cpu-credit-exhaustion]] during an unusually long batch run; the backlog drained within two hours.\n",
		},
		{
			UID: uid(40), Slug: "initech-account-onboarding-notes", Type: "note", Scope: "client-initech",
			Title: "Initech account onboarding notes",
			Tags:  []string{"customer/initech", "layer/queue", "layer/email", "layer/infra"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "This note is the single required read before a new engineer touches the Initech account"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-09-14",
			Body: oversizeBody(),
		},
	}
}
