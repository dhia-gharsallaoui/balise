package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// verbatimDefaultFiles are copied byte-for-byte from the real repo's defaults directory into
// the demo's own defaults directory. Every one of these is already fully generic — the eight
// type schemas, the agent-order list, the declared-scopes list, and the vendor facet name no
// real client, so there is nothing in them to genericize.
var verbatimDefaultFiles = []string{
	"types/decision.yaml", "types/entity.yaml", "types/gotcha.yaml", "types/incident.yaml",
	"types/issue.yaml", "types/note.yaml", "types/procedure.yaml", "types/state.yaml",
	"order.yaml", "scopes.yaml", "facets/vendor.yaml",
}

// exampleAsRealCopy is one ".example.yaml" sibling copied in as the real file the demo's
// defaults directory needs.
type exampleAsRealCopy struct{ src, dst string }

// exampleAsRealFiles: the real defaults/tenants.yaml, defaults/spaces.yaml and
// defaults/facets/customer.yaml (a real file always wins over its .example.yaml sibling per
// internal/registry/configfallback.go's realOrExample) hold the owner's actual client
// codenames and must never reach anything published — so the demo vault gets its own
// defaults directory built from the already-generic ".example.yaml" siblings instead.
var exampleAsRealFiles = []exampleAsRealCopy{
	{"tenants.example.yaml", "tenants.yaml"},
	{"spaces.example.yaml", "spaces.yaml"},
	{"facets/customer.example.yaml", "facets/customer.yaml"},
}

// demoLayerFacet is the demo's own facets/layer.yaml: the real one's values (network,
// compute, storage, identity, observability, iac, security) plus "agents" and "fabric" — the
// two values defaults/spaces.example.yaml's own filters reference that the shipped layer
// facet does not yet define. Adding them here, in the demo's own copy, means this generator
// never has to touch the real defaults/facets/layer.yaml.
const demoLayerFacet = `name: layer
values: {network: {}, compute: {}, storage: {}, identity: {}, observability: {}, iac: {}, security: {}, agents: {}, fabric: {}}
`

// writeDefaults assembles outDefaults (the demo's own defaults directory, wired via
// `balise reindex/serve --defaults`) from sourceDefaults (the repo's real defaults/), so the
// demo indexes and serves under a config that names only acme/globex/initech/umbrella and the
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
