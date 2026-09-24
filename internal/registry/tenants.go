package registry

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Tenants is the explicit, owner-editable list of customer/* facet values that name a real
// tenant, loaded from defaults/tenants.yaml. The customer/ facet namespace also carries a
// datacenter code and the owner's own internal categories (platform, intranet) — treating
// every customer/* facet as if it named a tenant conflates a site, and internal bookkeeping,
// with the actual security partition (which pages belong to which customer's scope). This is
// data rather than a hardcoded Go map because that partition is security-relevant, and the
// owner must be able to correct it without a recompile.
type Tenants struct{ names map[string]bool }

// LoadTenants reads defaults/tenants.yaml, or defaults/tenants.example.yaml when the real
// file is absent (see realOrExample) — a fresh clone still gets a working, generic default.
func LoadTenants(path string) (*Tenants, error) {
	path = realOrExample(path)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// A *pointer* to the slice, so an absent key is distinguishable from an explicitly
	// empty one. Both used to be rejected together, which made "this vault has no external
	// customers" unrepresentable: a single-owner or personal vault could not load its own
	// registry at all, and /api/settings failed outright.
	//
	// The guard's real purpose is to refuse a *silent* fallback to "nothing is a tenant",
	// since that would quietly move the security partition with no error anywhere. A
	// truncated file, a typo'd key or a null value give nil here and are still refused. An
	// explicit `tenants: []` is not silent -- it is the owner stating the partition is
	// empty -- so it is accepted, and Is() then answers false for everything, which is
	// exactly what was asked for.
	var raw struct {
		Tenants *[]string `yaml:"tenants"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw.Tenants == nil {
		return nil, fmt.Errorf("parse %s: tenants key is missing (use `tenants: []` to declare there are none)", path)
	}
	names := make(map[string]bool, len(*raw.Tenants))
	for _, name := range *raw.Tenants {
		names[name] = true
	}
	return &Tenants{names: names}, nil
}

// Is reports whether name is a declared tenant — i.e. a real customer, not a datacenter or
// one of the owner's own internal categories.
func (t *Tenants) Is(name string) bool { return t.names[name] }

// Names returns every declared tenant name, sorted for deterministic output. Tests that need
// to derive a scope allow-list (work plus client-<name> for each tenant) use this instead of
// hardcoding tenant names, so the same test works whether tenants.yaml holds the generic
// example tenants or the owner's real, gitignored list.
func (t *Tenants) Names() []string {
	names := make([]string, 0, len(t.names))
	for name := range t.names {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
