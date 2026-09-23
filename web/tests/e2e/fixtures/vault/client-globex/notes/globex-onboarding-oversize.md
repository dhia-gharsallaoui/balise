---
uid: 01JAAAAAAAAAAAAAAAAAAAAAA5
slug: globex-onboarding-oversize
type: note
scope: client-globex
title: Globex account onboarding notes for new engineers
tags: [customer/globex]
claims:
  - {text: New engineers must read this before touching the Globex environment, status: active, id: c1}
  - {text: The account has local process exceptions the general onboarding material omits, status: active, id: c2}
status: active
owner: dhia
last_verified: 2026-08-30
---
This note collects everything a new engineer needs before they touch the Globex environment for the first time. It exists because the account has enough local exceptions to the usual process that skipping it has caused real incidents in the past, and it is deliberately long: every section below has been added after someone learned the hard way that it was missing.

## Network segmentation

On the Globex account, network segmentation requires sign-off from the customer's security team. In practice this means that any engineer touching network segmentation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also needs a rollback plan documented before execution. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving network segmentation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Identity federation

On the Globex account, identity federation must be scheduled during the agreed maintenance window. In practice this means that any engineer touching identity federation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is tracked in the joint change calendar. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving identity federation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Expressroute peering

On the Globex account, expressroute peering depends on the shared responsibility matrix being current. In practice this means that any engineer touching expressroute peering on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also has a named owner on both sides of the engagement. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving expressroute peering, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Firewall policy

On the Globex account, firewall policy needs a rollback plan documented before execution. In practice this means that any engineer touching firewall policy on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is reviewed quarterly with the account team. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving firewall policy, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Dns delegation

On the Globex account, dns delegation is tracked in the joint change calendar. In practice this means that any engineer touching dns delegation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also produces an audit trail that the customer can request. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving dns delegation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Certificate rotation

On the Globex account, certificate rotation has a named owner on both sides of the engagement. In practice this means that any engineer touching certificate rotation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is gated behind a peer review of the proposed change. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving certificate rotation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Backup verification

On the Globex account, backup verification is reviewed quarterly with the account team. In practice this means that any engineer touching backup verification on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is validated against the onboarding checklist before go-live. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving backup verification, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Patch scheduling

On the Globex account, patch scheduling produces an audit trail that the customer can request. In practice this means that any engineer touching patch scheduling on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also requires sign-off from the customer's security team. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving patch scheduling, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Access review

On the Globex account, access review is gated behind a peer review of the proposed change. In practice this means that any engineer touching access review on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also must be scheduled during the agreed maintenance window. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving access review, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Incident escalation

On the Globex account, incident escalation is validated against the onboarding checklist before go-live. In practice this means that any engineer touching incident escalation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also depends on the shared responsibility matrix being current. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving incident escalation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Change approval

On the Globex account, change approval requires sign-off from the customer's security team. In practice this means that any engineer touching change approval on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also needs a rollback plan documented before execution. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving change approval, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Capacity planning

On the Globex account, capacity planning must be scheduled during the agreed maintenance window. In practice this means that any engineer touching capacity planning on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is tracked in the joint change calendar. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving capacity planning, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Vendor onboarding

On the Globex account, vendor onboarding depends on the shared responsibility matrix being current. In practice this means that any engineer touching vendor onboarding on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also has a named owner on both sides of the engagement. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving vendor onboarding, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Cost allocation

On the Globex account, cost allocation needs a rollback plan documented before execution. In practice this means that any engineer touching cost allocation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is reviewed quarterly with the account team. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving cost allocation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Monitoring coverage

On the Globex account, monitoring coverage is tracked in the joint change calendar. In practice this means that any engineer touching monitoring coverage on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also produces an audit trail that the customer can request. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving monitoring coverage, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Log retention

On the Globex account, log retention has a named owner on both sides of the engagement. In practice this means that any engineer touching log retention on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is gated behind a peer review of the proposed change. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving log retention, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Secrets rotation

On the Globex account, secrets rotation is reviewed quarterly with the account team. In practice this means that any engineer touching secrets rotation on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is validated against the onboarding checklist before go-live. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving secrets rotation, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Disaster recovery drills

On the Globex account, disaster recovery drills produces an audit trail that the customer can request. In practice this means that any engineer touching disaster recovery drills on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also requires sign-off from the customer's security team. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving disaster recovery drills, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Compliance evidence

On the Globex account, compliance evidence is gated behind a peer review of the proposed change. In practice this means that any engineer touching compliance evidence on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also must be scheduled during the agreed maintenance window. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving compliance evidence, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Runbook maintenance

On the Globex account, runbook maintenance is validated against the onboarding checklist before go-live. In practice this means that any engineer touching runbook maintenance on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also depends on the shared responsibility matrix being current. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving runbook maintenance, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

The Globex engagement introduced its own naming conventions for resource groups, subscriptions, and tags, and every new engineer spends their first week mapping those conventions onto the vendor's own internal taxonomy before touching production.

Escalation paths differ from the standard internal ladder: a sev-1 incident on this account pages the account lead directly, in addition to the normal on-call rotation, because the customer's own operations team expects a human response within fifteen minutes.

Every credential used against the customer's tenant is stored in the dedicated vault namespace for this engagement, never in the shared namespace used for internal systems, and access to that namespace is reviewed monthly rather than quarterly.

The customer's change window is narrower than most: Tuesday and Thursday evenings only, local time, and any change proposed outside that window needs a documented exception signed by both the account lead and the customer's change manager.

Onboarding a new engineer onto this account also means walking them through the customer's own ticketing system, which is separate from the internal one, so that status updates reach the customer without someone manually copying notes between two systems.

None of this replaces the general onboarding material every new hire already receives; it only covers the parts that are specific to this one account and that the general material cannot anticipate. Read it once in full before the first week on the account, and come back to individual sections as questions come up during that first month.
## Data residency

On the Globex account, data residency must be scheduled during the agreed maintenance window. In practice this means that any engineer touching data residency on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is tracked in the joint change calendar. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving data residency, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Key management

On the Globex account, key management depends on the shared responsibility matrix being current. In practice this means that any engineer touching key management on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also has a named owner on both sides of the engagement. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving key management, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Audit logging cadence

On the Globex account, audit logging cadence needs a rollback plan documented before execution. In practice this means that any engineer touching audit logging cadence on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also is reviewed quarterly with the account team. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving audit logging cadence, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Service account provisioning

On the Globex account, service account provisioning is gated behind a peer review of the proposed change. In practice this means that any engineer touching service account provisioning on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also must be scheduled during the agreed maintenance window. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving service account provisioning, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Third-party integrations

On the Globex account, third-party integrations produces an audit trail that the customer can request. In practice this means that any engineer touching third-party integrations on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also requires sign-off from the customer's security team. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving third-party integrations, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.

## Release freeze windows

On the Globex account, release freeze windows is validated against the onboarding checklist before go-live. In practice this means that any engineer touching release freeze windows on this account should expect a slower cycle than on an internal-only project: the same change on an internal system would typically move faster, but here it also depends on the shared responsibility matrix being current. New engineers consistently underestimate this the first time, assuming the internal default process applies unchanged, and the resulting friction is the single most common source of avoidable delay reported by engineers who are new to the account. Plan the extra review time into any estimate involving release freeze windows, and raise it explicitly with the customer's counterpart rather than assuming it will be absorbed silently.
