package main

// acmePages is the "client-acme" scope: Acme's checkout and payments knowledge, layered on
// top of the shared work-scope platform.
func acmePages() []page {
	return []page{
		{
			UID: uid(17), Slug: "acme-checkout-payment-integration-primary", Type: "state", Scope: "client-acme",
			Title: "Acme checkout payment integration, primary",
			About: []string{"acme-stripe-webhook-retries-duplicate-confirmation-emails"},
			Tags:  []string{"customer/acme", "layer/payments", "layer/backend", "vendor/stripe"},
			AsOf:  "2026-08-01",
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Acme's checkout charges cards through Stripe's hosted payment intents flow"},
				{ID: "c2", Status: "active", Text: "A confirmation email sends only after Stripe's payment_intent.succeeded webhook lands"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-08",
			Body: "Acme's checkout uses Stripe's hosted payment intents flow end to end. The confirmation email only sends once the payment_intent.succeeded webhook lands, which is exactly the step [[acme-stripe-webhook-retries-duplicate-confirmation-emails]] describes going wrong under retry.\n",
		},
		{
			UID: uid(18), Slug: "acme-stripe-webhook-retries-duplicate-confirmation-emails", Type: "gotcha", Scope: "client-acme",
			Title: "Stripe webhook retries duplicate checkout confirmation emails",
			Tags:  []string{"customer/acme", "layer/payments", "vendor/stripe"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Stripe retries a webhook delivery whenever the receiving endpoint responds slowly, not only on an outright failure"},
				{ID: "c2", Status: "active", Text: "The checkout handler resent the confirmation email on every retry of the same event"},
				{ID: "c3", Status: "active", Text: "A single successful charge could trigger three or more duplicate confirmation emails to the customer"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-08",
			Body: "Stripe retries a webhook delivery whenever the endpoint is slow to acknowledge it, not only when it fails outright. The checkout handler on [[acme-checkout-payment-integration-primary]] resent the confirmation email on every retry of the same payment_intent.succeeded event, so one successful charge could produce three or more duplicate emails.\n\nFix: key the send on the Stripe event ID and skip any event ID already processed.\n",
		},
		{
			UID: uid(19), Slug: "acme-checkout-v2-flag-reused-across-experiments", Type: "gotcha", Scope: "client-acme",
			Title:        "Checkout v2 flag key was reused across two different experiments",
			SupersededBy: "acme-standardize-flag-naming-scheme",
			Tags:         []string{"customer/acme", "layer/release"},
			Claims: []claim{
				{ID: "c1", Status: "superseded", AsOf: "2026-08-05", Text: "The flag key checkout-v2 was created for a layout experiment in June"},
				{ID: "c2", Status: "superseded", AsOf: "2026-08-05", Text: "A second, unrelated pricing experiment reused the same checkout-v2 key in July"},
				{ID: "c3", Status: "superseded", AsOf: "2026-08-05", Text: "Flipping the flag off to end the pricing test also silently reverted the unrelated layout change"},
			},
			Status: "superseded", Owner: "Priya Nair", LastVerified: "2026-08-05",
			Body: "The flag key checkout-v2, created for a June layout experiment, was reused by an unrelated July pricing experiment. Turning the flag off to end the pricing test also silently reverted the layout change nobody meant to touch. Retired in favor of [[acme-standardize-flag-naming-scheme]].\n",
		},
		{
			UID: uid(20), Slug: "acme-standardize-flag-naming-scheme", Type: "decision", Scope: "client-acme",
			Title: "Standardize on a namespaced feature flag naming scheme",
			Tags:  []string{"customer/acme", "layer/release"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Every new flag key is namespaced as team.experiment.ticket"},
				{ID: "c2", Status: "active", Text: "A namespace collision is now a review-time rejection instead of a runtime surprise"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-08-06",
			Body: "Adopted a team.experiment.ticket namespace for every new flag key after [[acme-checkout-v2-flag-reused-across-experiments]]. [[acme-launch-checkout-feature-flag]] follows this scheme for new checkout flags.\n",
		},
		{
			UID: uid(21), Slug: "acme-launch-checkout-feature-flag", Type: "procedure", Scope: "client-acme",
			Title: "Launch a new checkout feature flag",
			Tags:  []string{"customer/acme", "layer/release"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Every new flag is registered with a team.experiment.ticket key before it ships to any environment"},
				{ID: "c2", Status: "active", Text: "A rollout plan with target percentage and rollback trigger is attached before the flag reaches production"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-08-20",
			Body: "1. Register the flag under a team.experiment.ticket key, per [[acme-standardize-flag-naming-scheme]].\n2. Attach a rollout plan with a target percentage and an explicit rollback trigger.\n3. Ship to staging first, then ramp production behind the plan.\n",
		},
		{
			UID: uid(22), Slug: "acme-checkout-error-rate-alert-noise", Type: "issue", Scope: "client-acme",
			Title: "Checkout error-rate alert pages on-call too often",
			Tags:  []string{"customer/acme", "layer/observability", "vendor/datadog"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "The alert pages whenever checkout's error rate crosses one percent for any thirty-second window"},
				{ID: "c2", Status: "active", Text: "It pages whenever the error rate stays above the threshold for two minutes, which catches routine deploy-time blips"},
			},
			Status: "open", Owner: "Priya Nair", LastVerified: "2026-09-14",
			Body: "The checkout error-rate alert in Datadog pages whenever the error rate crosses one percent and stays above it for two minutes, which routinely fires during ordinary deploy-time traffic dips rather than a real problem.\n",
		},
		{
			UID: uid(23), Slug: "acme-black-friday-checkout-surge-2026", Type: "incident", Scope: "client-acme",
			Title: "Acme Black Friday checkout surge, 2026",
			Tags:  []string{"customer/acme", "layer/payments", "vendor/stripe"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Checkout traffic peaked at roughly nine times the daily average during the Black Friday surge"},
				{ID: "c2", Status: "active", Text: "Stripe's webhook retries during the surge triggered the known duplicate-email gotcha at high volume"},
				{ID: "c3", Status: "active", Text: "No customer was double-charged; only the confirmation email sent more than once"},
			},
			Status: "resolved", Owner: "Priya Nair", LastVerified: "2026-08-30",
			Body: "Checkout traffic peaked at roughly nine times the daily average. The surge in Stripe webhook retries reproduced [[acme-stripe-webhook-retries-duplicate-confirmation-emails]] at high volume, so a large batch of customers received duplicate confirmation emails. No customer was double-charged.\n",
		},
		{
			UID: uid(24), Slug: "acme-account-escalation-contacts", Type: "note", Scope: "client-acme",
			Title: "Acme account escalation contacts",
			Tags:  []string{"customer/acme", "layer/observability"},
			Claims: []claim{
				{ID: "c1", Status: "active", Text: "Acme's primary technical contact is looped in on any checkout-affecting incident within fifteen minutes"},
				{ID: "c2", Status: "active", Text: "A Sev1 automatically opens a joint incident channel with Acme's own on-call"},
			},
			Status: "active", Owner: "Priya Nair", LastVerified: "2026-09-14",
			Body: "Acme's primary technical contact is looped in within fifteen minutes of any checkout-affecting incident. A Sev1 automatically opens a joint incident channel with Acme's own on-call rotation.\n",
		},
	}
}
