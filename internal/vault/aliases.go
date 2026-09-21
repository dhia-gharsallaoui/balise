package vault

import "sort"

// Collision records an alias claimed by more than one page in the same scope.
type Collision struct {
	Scope string
	Alias string
	UIDs  []string
}

type aliasKey struct{ scope, alias string }

// AliasRegistry maps aliases to uids per scope. It is immutable: Register returns a
// new registry and leaves the receiver untouched, so an indexing pass can build one
// incrementally without any call site observing a half-built state.
type AliasRegistry struct {
	exact      map[aliasKey]map[string]struct{}
	normalised map[aliasKey]map[string]struct{}
}

// Register returns a copy of the registry with alias → uid added in scope.
func (r AliasRegistry) Register(alias, uid, scope string) AliasRegistry {
	next := AliasRegistry{
		exact:      copyIndex(r.exact),
		normalised: copyIndex(r.normalised),
	}
	add(next.exact, aliasKey{scope, alias}, uid)
	add(next.normalised, aliasKey{scope, Normalise(alias)}, uid)
	return next
}

// LookupExact returns the uid for an exact alias, or "" when absent or ambiguous.
func (r AliasRegistry) LookupExact(alias, scope string) string {
	return single(r.exact[aliasKey{scope, alias}])
}

// LookupNormalised returns the uid for a normalised reference, or "" when absent or ambiguous.
func (r AliasRegistry) LookupNormalised(ref, scope string) string {
	return single(r.normalised[aliasKey{scope, Normalise(ref)}])
}

// Collisions lists every alias claimed by more than one page in a scope.
func (r AliasRegistry) Collisions() []Collision {
	var out []Collision
	for key, uids := range r.exact {
		if len(uids) < 2 {
			continue
		}
		list := make([]string, 0, len(uids))
		for uid := range uids {
			list = append(list, uid)
		}
		sort.Strings(list)
		out = append(out, Collision{Scope: key.scope, Alias: key.alias, UIDs: list})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

func copyIndex(src map[aliasKey]map[string]struct{}) map[aliasKey]map[string]struct{} {
	dst := make(map[aliasKey]map[string]struct{}, len(src)+1)
	for key, uids := range src {
		inner := make(map[string]struct{}, len(uids))
		for uid := range uids {
			inner[uid] = struct{}{}
		}
		dst[key] = inner
	}
	return dst
}

func add(index map[aliasKey]map[string]struct{}, key aliasKey, uid string) {
	if index[key] == nil {
		index[key] = map[string]struct{}{}
	}
	index[key][uid] = struct{}{}
}

// single returns the only uid in the set, or "" when the set is empty or ambiguous.
func single(uids map[string]struct{}) string {
	if len(uids) != 1 {
		return ""
	}
	for uid := range uids {
		return uid
	}
	return ""
}
