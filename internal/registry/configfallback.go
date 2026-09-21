package registry

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// realOrExample returns path unchanged when it exists on disk. Otherwise it returns the
// path's ".example" sibling (defaults/tenants.yaml -> defaults/tenants.example.yaml) when
// that exists, so a fresh clone — which has only the tracked .example file, since the real
// one holds the owner's actual customer data and is gitignored — still gets a working
// default. When neither exists, path is returned unchanged so the caller's normal
// file-not-found error names the file the caller actually asked for.
func realOrExample(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	ext := filepath.Ext(path)
	example := strings.TrimSuffix(path, ext) + ".example" + ext
	if _, err := os.Stat(example); err == nil {
		return example
	}
	return path
}

// facetConfigFiles lists the facet YAML files a directory contributes, applying the same
// real-beats-example precedence as realOrExample but per-file rather than for a single
// path: every non-example *.yaml is included, and a *.example.yaml is included only when
// its real counterpart (the same base name without ".example") is not present in dir. This
// lets facets/customer.yaml (real, gitignored) and facets/customer.example.yaml (tracked)
// live side by side, while facets/vendor.yaml — which has no .example sibling because it
// carries no customer-identifying data — loads exactly as before.
func facetConfigFiles(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}

	var reals, examples []string
	hasReal := map[string]bool{}
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".example.yaml") {
			examples = append(examples, path)
			continue
		}
		reals = append(reals, path)
		hasReal[strings.TrimSuffix(base, ".yaml")] = true
	}

	out := append([]string(nil), reals...)
	for _, path := range examples {
		base := strings.TrimSuffix(filepath.Base(path), ".example.yaml")
		if !hasReal[base] {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}
