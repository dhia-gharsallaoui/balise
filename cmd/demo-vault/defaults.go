package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// verbatimDefaultFiles are copied byte-for-byte from the real repo's defaults directory into
// the demo's own defaults directory. Every one of these is already fully generic — the eight
// type schemas, the agent-order list, and the declared-scopes list name no real client, so
// there is nothing in them to genericize. facets/vendor.yaml is deliberately absent from this
// list: the real repo's copy only declares azure/fortinet, so the demo owns its own copy (see
// demoVendorFacet below) instead of shipping Azure vendor names into unrelated content.
var verbatimDefaultFiles = []string{
	"types/decision.yaml", "types/gotcha.yaml", "types/incident.yaml", "types/issue.yaml",
	"types/memory.yaml", "types/note.yaml", "types/procedure.yaml", "types/state.yaml",
	"order.yaml", "scopes.yaml",
}

// exampleAsRealCopy is one ".example.yaml" sibling copied in as the real file the demo's
// defaults directory needs.
type exampleAsRealCopy struct{ src, dst string }

// exampleAsRealFiles: the real defaults/tenants.yaml and defaults/facets/customer.yaml (a real
// file always wins over its .example.yaml sibling per internal/registry/configfallback.go's
// realOrExample) hold the owner's actual client codenames and must never reach anything
// published — so the demo vault gets its own defaults directory built from the already-generic
// ".example.yaml" siblings instead. spaces.yaml is deliberately not copied this way: the real
// repo's spaces.example.yaml is built around the Azure/IaC tag vocabulary, not this generator's
// own layer/vendor/customer tags, so the demo owns its own copy instead (see demoSpaces below).
var exampleAsRealFiles = []exampleAsRealCopy{
	{"tenants.example.yaml", "tenants.yaml"},
	{"facets/customer.example.yaml", "facets/customer.yaml"},
}

// demoLayerFacet is the demo's own facets/layer.yaml, matching the tag vocabulary this
// generator's content and demoSpaces below were written against. It intentionally does not
// try to reuse the real repo's shipped layer facet (network, compute, storage, iac, ... —
// Azure/IaC oriented) since the demo's subject matter is unrelated backend/frontend/platform
// engineering; owning this file here means the generator never has to touch the real
// defaults/facets/layer.yaml.
const demoLayerFacet = `name: layer
values: {backend: {}, frontend: {}, database: {}, cache: {}, queue: {}, ci: {}, release: {}, observability: {}, security: {}, identity: {}, payments: {}, email: {}, infra: {}, agents: {}}
`

// demoVendorFacet is the demo's own facets/vendor.yaml. The real repo's copy only declares
// azure and fortinet, neither of which this content uses, so the demo owns a full replacement
// naming the vendors its pages actually reference, with a couple of light aliases for the
// vendors most likely to be typed differently.
const demoVendorFacet = `name: vendor
values: {postgres: {aliases: [postgresql, pg]}, redis: {}, kafka: {}, rabbitmq: {aliases: [amqp]}, stripe: {}, sendgrid: {}, okta: {}, github_actions: {aliases: [gha]}, docker: {}, nginx: {}, datadog: {}, pagerduty: {}}
`

// demoSpaces is the demo's own spaces.yaml, replacing the real repo's Azure/IaC-themed
// defaults/spaces.example.yaml with a space tree over this generator's own layer/vendor tags.
// Each scope root's children are verified non-empty against the actual generated content in
// content_work.go/content_acme.go/content_globex.go/content_initech.go.
const demoSpaces = `
- name: Platform
  scope: work
  filter:
    tags: []
  children:
    - name: Backend
      filter:
        tags: ["layer/backend"]
    - name: Database
      filter:
        tags: ["layer/database"]
    - name: Caching
      filter:
        tags: ["layer/cache"]
    - name: Event Streaming
      filter:
        tags: ["layer/queue"]
    - name: CI/CD
      filter:
        tags: ["layer/ci"]
    - name: Security
      filter:
        tags: ["layer/security"]
    - name: Observability
      filter:
        tags: ["layer/observability"]
    - name: Infra
      filter:
        tags: ["layer/infra"]
    - name: Agents
      filter:
        tags: ["layer/agents"]
- name: Globex
  scope: client-globex
  filter:
    tags: []
  children:
    - name: Event Streaming
      filter:
        tags: ["layer/queue"]
    - name: Identity
      filter:
        tags: ["layer/identity"]
    - name: Backend
      filter:
        tags: ["layer/backend"]
    - name: Security
      filter:
        tags: ["layer/security"]
    - name: Observability
      filter:
        tags: ["layer/observability"]
- name: Acme
  scope: client-acme
  filter:
    tags: []
  children:
    - name: Payments
      filter:
        tags: ["layer/payments"]
    - name: Feature Flags
      filter:
        tags: ["layer/release"]
    - name: Observability
      filter:
        tags: ["layer/observability"]
- name: Initech
  scope: client-initech
  filter:
    tags: []
  children:
    - name: Workers & Queue
      filter:
        tags: ["layer/queue"]
    - name: Frontend
      filter:
        tags: ["layer/frontend"]
    - name: Email
      filter:
        tags: ["layer/email"]
    - name: Infra
      filter:
        tags: ["layer/infra"]
`

// writeDefaults assembles outDefaults (the demo's own defaults directory, wired via
// `balise reindex/serve --defaults`) from sourceDefaults (the repo's real defaults/), so the
// demo indexes and serves under a config that names only acme/globex/initech and the
// generic space/facet trees the rest of this generator's content was written against.
func writeDefaults(sourceDefaults, outDefaults string) error {
	for _, rel := range verbatimDefaultFiles {
		if err := copyFile(filepath.Join(sourceDefaults, rel), filepath.Join(outDefaults, rel)); err != nil {
			return err
		}
	}
	for _, c := range exampleAsRealFiles {
		if err := copyFile(filepath.Join(sourceDefaults, c.src), filepath.Join(outDefaults, c.dst)); err != nil {
			return err
		}
	}
	layerPath := filepath.Join(outDefaults, "facets", "layer.yaml")
	if err := os.MkdirAll(filepath.Dir(layerPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(layerPath), err)
	}
	if err := os.WriteFile(layerPath, []byte(demoLayerFacet), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", layerPath, err)
	}

	vendorPath := filepath.Join(outDefaults, "facets", "vendor.yaml")
	if err := os.MkdirAll(filepath.Dir(vendorPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(vendorPath), err)
	}
	if err := os.WriteFile(vendorPath, []byte(demoVendorFacet), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", vendorPath, err)
	}

	spacesPath := filepath.Join(outDefaults, "spaces.yaml")
	if err := os.MkdirAll(filepath.Dir(spacesPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(spacesPath), err)
	}
	if err := os.WriteFile(spacesPath, []byte(demoSpaces), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", spacesPath, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}
