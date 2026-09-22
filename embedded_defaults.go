// Package balise embeds the generic, tracked subset of defaults/ so that a compiled
// balise binary is self-sufficient: it can resolve --defaults without a directory on
// disk at all, which is what lets `balise mcp --stdio <vault>` (and every other
// subcommand) work when spawned by an MCP host from an arbitrary directory with no
// `defaults/` anywhere nearby. See cmd/balise's resolveDefaultsDir for how this is used
// as the last resort in the --defaults resolution order.
//
// Only the tracked, generic files are embedded here — never defaults/tenants.yaml,
// defaults/spaces.yaml or defaults/facets/customer.yaml, which hold the owner's real
// customer data and are gitignored precisely so they never leave this machine (see
// .gitignore). Embedding those would ship real customer data inside every compiled
// binary, including ones handed to other people. The tracked .example siblings are
// embedded in their place, so the embedded fallback behaves exactly like a fresh
// clone's defaults/ would — generic and safe — never like the owner's own configured
// vault.
//
// defaults/fixtures/ is deliberately not embedded: nothing read through --defaults at
// runtime (registry.Load, LoadOrder, LoadFacets, LoadSpaces, LoadTenants,
// LoadDeclaredScopes) touches it — it exists only for indexer tests, which glob the
// real directory directly.
package balise

import "embed"

// EmbeddedDefaults holds the generic defaults/ subset, under the same "defaults/..."
// relative layout every registry loader already expects (types/, facets/, order.yaml,
// scopes.yaml, spaces.example.yaml, tenants.example.yaml). cmd/balise's
// extractEmbeddedDefaults materializes it onto a real temp directory before use, since
// those loaders read through os.ReadFile/filepath.Glob against a filesystem path, not
// an fs.FS.
//
//go:embed defaults/order.yaml defaults/scopes.yaml defaults/spaces.example.yaml defaults/tenants.example.yaml
//go:embed defaults/types
//go:embed defaults/facets/layer.yaml defaults/facets/vendor.yaml defaults/facets/customer.example.yaml
var EmbeddedDefaults embed.FS
