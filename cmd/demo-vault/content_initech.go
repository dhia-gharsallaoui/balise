package main

import (
	"fmt"
	"strings"
)

// oversizeTopic is one repetitive section of the deliberately oversize onboarding note below.
// The template mirrors defaults/fixtures/globex-onboarding-oversize.md's own structure (a
// fixed sentence shape, a different topic substituted each time) but reworded for Initech's
// compute/storage account rather than Globex's network account, so the two oversize fixtures
// in this codebase are not verbatim duplicates of each other.
type oversizeTopic struct {
	heading string
	topic   string
	reason  string
	extra   string
}

var initechOnboardingTopics = []oversizeTopic{
	{"VM sizing requests", "VM sizing requests", "require a cost-center approval before the resize is scheduled", "needs a rollback SKU recorded before execution"},
	{"Storage account provisioning", "storage account provisioning", "must go through the shared storage naming reservation first", "is tracked in the joint capacity spreadsheet"},
	{"Backup retention changes", "backup retention changes", "need the retention period confirmed against the contract minimum", "is reviewed by the account's compliance contact"},
	{"Disk encryption key rotation", "disk encryption key rotation", "must be scheduled during the agreed maintenance window", "produces an audit trail the customer can request"},
	{"VM extension installs", "VM extension installs", "are blocked on non-approved extension publishers by policy", "has a named owner on both sides of the engagement"},
	{"Compute subnet NSG changes", "compute subnet NSG changes", "require a peer review of the proposed rule diff", "is gated behind a change ticket reference"},
	{"Storage lifecycle rule changes", "storage lifecycle rule changes", "depend on the immutability status being checked first", "is tracked in the joint change calendar"},
	{"Storage RBAC role assignment", "storage RBAC role assignment", "must not grant Owner at the account scope directly", "needs a rollback plan documented before execution"},
	{"Tag governance", "tag governance", "follows the shared cost-center tag schema, not a local one", "is reviewed quarterly with the account team"},
	{"Monitoring alert rule changes", "monitoring alert rule changes", "need a paging threshold sign-off from the account owner", "is tracked in the joint change calendar"},
	{"Capacity planning reviews", "capacity planning reviews", "happen monthly and require the prior month's actuals attached", "has a named owner on both sides of the engagement"},
	{"Batch job scheduling changes", "batch job scheduling changes", "must avoid the nightly reconciliation window entirely", "produces an audit trail the customer can request"},
	{"Credential rotation", "credential rotation", "requires the old credential kept valid for one hour", "is gated behind a peer review of the proposed change"},
	{"VM image gallery updates", "VM image gallery updates", "must be validated against the B-series exception list first", "needs a rollback plan documented before execution"},
	{"Quota increase requests", "quota increase requests", "go through the shared subscription quota tracker, not ad hoc", "is reviewed by the account's compliance contact"},
	{"Cost allocation tag changes", "cost allocation tag changes", "must match the billing sync's expected tag keys exactly", "is tracked in the joint change calendar"},
	{"Snapshot retention", "snapshot retention", "follows the same minimum as backup retention, not a shorter one", "has a named owner on both sides of the engagement"},
	{"Storage container provisioning", "storage container provisioning", "requires the naming reservation step before creation", "is reviewed quarterly with the account team"},
	{"VM extension timeout handling", "VM extension timeout handling", "needs the known resize-timeout gotcha checked first", "produces an audit trail the customer can request"},
	{"Batch backlog escalation", "batch backlog escalation", "pages the account owner directly, not the shared on-call", "is gated behind a change ticket reference"},
}

// oversizeBody renders the onboarding note's body: an intro paragraph plus one section per
// topic, each following the same fixed sentence shape. This is deliberately long — the task
// calls for one genuine oversize page, not a manufactured list of problems, so the length
// comes from a real (if repetitive) onboarding document shape rather than padding.
func oversizeBody() string {
	var b strings.Builder
	b.WriteString("This note collects everything a new engineer needs before they touch the " +
		"Initech environment for the first time. It exists because the account has enough " +
		"local exceptions to the usual process that skipping it has caused real delays in the " +
		"past, and it is deliberately long: every section below has been added after someone " +
		"learned the hard way that it was missing.\n")
	for _, t := range initechOnboardingTopics {
		fmt.Fprintf(&b, "\n## %s\n\n", t.heading)
		fmt.Fprintf(&b,
			"On the Initech account, %s %s. In practice this means that any engineer touching "+
				"%s on this account should expect a slower cycle than on an internal-only "+
				"project: the same change on an internal system would typically move faster, "+
				"but here it also %s. New engineers consistently underestimate this the first "+
				"time, assuming the internal default process applies unchanged, and the "+
				"resulting friction is the single most common source of avoidable delay "+
				"reported by engineers who are new to the account. Plan the extra review time "+
				"into any estimate involving %s, and raise it explicitly with the customer's "+
				"counterpart rather than assuming it will be absorbed silently.\n",
			t.topic, t.reason, t.topic, t.extra, t.topic)
	}
	return b.String()
}

// initechPages is the "client-initech" scope: a shared compute/storage baseline, a B-series
// VM gotcha, and the one deliberately oversize page the task brief calls for.
func initechPages() []page {
	return []page{
		{
			UID: uid(33), Slug: "initech-shared-compute-baseline", Type: "state", Scope: "client-initech",
			Title: "Initech shared compute baseline",
			Tags:  []string{"customer/initech", "layer/compute", "layer/storage"},
			AsOf:  "2026-08-05",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "All workloads run on B-series VMs backed by one shared storage account"},
				{ID: "c2", Status: "active", Text: "Storage account consolidation reduced monthly cost by roughly twelve percent"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-09-01",
			Body: "Initech's compute baseline standardizes on B-series VMs against one shared " +
				"storage account, per [[initech-consolidate-storage-into-shared-account]] and " +
				"[[initech-standardize-on-b-series-vms]].\n",
		},
		{
			UID: uid(34), Slug: "initech-vm-extension-timeout-on-resize", Type: "gotcha", Scope: "client-initech",
			Title: "VM extension install times out during a concurrent resize",
			Tags:  []string{"customer/initech", "layer/compute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Resizing a VM while an extension install is in flight times it out at ninety seconds"},
				{ID: "c2", Status: "active", Text: "The extension shows as failed but the underlying install often still completed"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-09-10",
			Body: "Starting a resize while a VM extension install is still running causes the " +
				"extension agent to time out at ninety seconds and report failure, even though " +
				"the install frequently finished underneath it. Always wait for extension " +
				"status to settle before resizing.\n",
		},
		{
			UID: uid(35), Slug: "initech-standardize-on-b-series-vms", Type: "decision", Scope: "client-initech",
			Title: "Standardize Initech workloads on B-series VMs",
			Tags:  []string{"customer/initech", "layer/compute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "B-series burstable VMs replace the prior mixed D-series and B-series fleet"},
				{ID: "c2", Status: "active", Text: "CPU credit exhaustion under sustained load is the known tradeoff"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-08-28",
			Body: "Standardized on B-series burstable VMs for cost, accepting the credit " +
				"exhaustion tradeoff tracked in [[initech-b-series-cpu-credit-exhaustion]]. " +
				"[[initech-vm-extension-timeout-on-resize]] applies to any B-series resize the " +
				"same as it did to the prior fleet.\n",
		},
		{
			UID: uid(36), Slug: "initech-consolidate-storage-into-shared-account", Type: "decision", Scope: "client-initech",
			Title: "Consolidate Initech storage into one shared account",
			Tags:  []string{"customer/initech", "layer/storage"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Six per-workload storage accounts consolidated into one shared account"},
				{ID: "c2", Status: "active", Text: "New containers are provisioned into the shared account by default"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-08-29",
			Body: "Consolidated six per-workload storage accounts into the shared baseline in " +
				"[[initech-shared-compute-baseline]]. New containers now provision into the " +
				"shared account by default per " +
				"[[initech-provision-new-app-storage-container]].\n",
		},
		{
			UID: uid(37), Slug: "initech-provision-new-app-storage-container", Type: "procedure", Scope: "client-initech",
			Title: "Provision a new app storage container for Initech",
			Tags:  []string{"customer/initech", "layer/storage"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Create the container inside the shared storage account, never a new account"},
				{ID: "c2", Status: "active", Text: "Apply the standard lifecycle rule template before the first upload"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-09-02",
			Body: "1. Create the container inside the shared account from " +
				"[[initech-consolidate-storage-into-shared-account]] — never provision a new " +
				"standalone account.\n" +
				"2. Apply the standard lifecycle rule template.\n" +
				"3. Confirm the workload's VM is on the B-series baseline from " +
				"[[initech-standardize-on-b-series-vms]] before pointing it at the new " +
				"container.\n",
		},
		{
			UID: uid(38), Slug: "initech-b-series-cpu-credit-exhaustion", Type: "issue", Scope: "client-initech",
			Title: "B-series CPU credit exhaustion under sustained batch load",
			Tags:  []string{"customer/initech", "layer/compute"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Sustained batch load exhausts CPU credits within forty minutes on B2ms"},
				{ID: "c2", Status: "active", Text: "Once exhausted, throughput drops to the baseline performance floor"},
			},
			Status: "open", Owner: "Leo Ferreira", LastVerified: "2026-09-13",
			Body: "Batch workloads running sustained load on B2ms instances exhaust their CPU " +
				"credit balance within about forty minutes, after which throughput drops to the " +
				"instance's baseline floor. This is the tradeoff accepted in " +
				"[[initech-standardize-on-b-series-vms]]; " +
				"[[initech-batch-job-backlog-2026-06]] is the incident it caused.\n",
		},
		{
			UID: uid(39), Slug: "initech-batch-job-backlog-2026-06", Type: "incident", Scope: "client-initech",
			Title: "Initech batch job backlog, June 2026",
			Tags:  []string{"customer/initech", "layer/compute", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Nightly batch queue backed up four hours after credit exhaustion throttled throughput"},
				{ID: "c2", Status: "active", Text: "A D-series exception was granted for the two largest batch workloads"},
			},
			Status: "monitoring", Owner: "Leo Ferreira", LastVerified: "2026-09-13",
			Body: "The nightly batch queue backed up roughly four hours after " +
				"[[initech-b-series-cpu-credit-exhaustion]] throttled several concurrent jobs to " +
				"baseline performance. A D-series exception was granted for the two largest " +
				"workloads per [[initech-standardize-on-b-series-vms]]; backlog burn-down is " +
				"still being monitored.\n",
		},
		{
			UID: uid(40), Slug: "initech-account-onboarding-notes", Type: "note", Scope: "client-initech",
			Title: "Initech account onboarding notes for new engineers",
			Tags:  []string{"customer/initech"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "New engineers must read this before touching the Initech environment"},
				{ID: "c2", Status: "active", Text: "The account has local process exceptions the general onboarding material omits"},
			},
			Status: "active", Owner: "Leo Ferreira", LastVerified: "2026-08-30",
			Body: oversizeBody(),
		},
	}
}
