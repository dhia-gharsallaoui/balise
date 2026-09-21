package vault

// ScopeIndex maps a reference's normalised form to the set of scopes it is known under —
// built from every page's own slug plus whatever aliases it declares. It exists so two
// different callers can classify a dangling reference as "cross-scope" (the target exists,
// just under a different scope — the security boundary in 04 section 14 working as intended)
// versus genuinely missing, without disagreeing about what "exists elsewhere" means:
// cli.Reindex builds one of these by walking the vault's git tree while a reindex is in
// flight, and store.Queries.SlugScopeIndex builds an equivalent one from whatever is
// currently persisted, for a caller (a later reindex's own printed report, or Home's "Needs
// attention" block) that only has storage to read, not a live git walk. Keeping the type and
// its two operations here, rather than reimplementing the same map-of-sets logic in each
// caller, is what keeps the two agree automatically instead of by convention.
type ScopeIndex map[string]map[string]bool

// Record notes that ref, in its normalised form, exists in scope.
func (idx ScopeIndex) Record(ref, scope string) {
	key := Normalise(ref)
	if idx[key] == nil {
		idx[key] = map[string]bool{}
	}
	idx[key][scope] = true
}

// CrossScope reports whether ref, normalised, is known under some scope other than
// ownScope. ownScope is excluded from the check because a same-scope match would mean the
// reference should already have resolved — it could not be dangling in the first place — so
// only a match under a different scope means anything here.
func (idx ScopeIndex) CrossScope(ref, ownScope string) bool {
	for scope := range idx[Normalise(ref)] {
		if scope != ownScope {
			return true
		}
	}
	return false
}
