// Package search holds ranking concerns that are not SQL.
//
// 04 section 11 defines coverage against a calibrated similarity threshold that needs
// embeddings and an eval set. Neither exists in this slice, so AssessCoverage is a
// deliberately cruder mechanical rule, to be replaced at M1. It exists at all because
// both arms of experiment 3c hallucinated identically on thin topics, and the UI must be
// able to say "I have nothing good here" from day one.
package search

import (
	"regexp"
	"strings"

	"github.com/dhia/balise/internal/store"
)

const minTermLength = 3

var stopwords = map[string]bool{}

var word = regexp.MustCompile(`[a-z0-9]+`)

func init() {
	for _, w := range strings.Fields(`
		a an and are as at be but by do does for from had has have how i if in is it its of on or
		our so than that the their then there these they this to was we what when where which who
		why will with you your`) {
		stopwords[w] = true
	}
}

// AssessCoverage returns "ok" when at least one hit shares a meaningful term with the
// query, and "low" otherwise.
func AssessCoverage(query string, hits []store.Hit) string {
	wanted := terms(query)
	if len(hits) == 0 || len(wanted) == 0 {
		return "low"
	}
	for _, hit := range hits {
		text := strings.Join(hit.MatchedClaims, " ")
		if strings.TrimSpace(text) == "" {
			text = hit.Title
		}
		for term := range terms(text) {
			if wanted[term] {
				return "ok"
			}
		}
	}
	return "low"
}

func terms(text string) map[string]bool {
	out := map[string]bool{}
	for _, match := range word.FindAllString(strings.ToLower(text), -1) {
		if len(match) > minTermLength-1 && !stopwords[match] {
			out[match] = true
		}
	}
	return out
}
