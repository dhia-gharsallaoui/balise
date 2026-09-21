// Package compile implements Balise's compile pipeline (04-technical-spec-v1.md
// section 9): LLM-backed tasks that read pages and the type registry and propose
// changes into review/<id>.md files. Compile tasks never write to typed pages
// directly — acceptance of a proposal is a separate, human-in-the-loop step that
// this package does not implement.
//
// extract_claims (section 9.1) is the first task: a page carries at most one
// claim on import (internal/importer records exactly one "hook" claim per page),
// and this task proposes the 1-6 claims a page's body actually supports, each a
// statement rather than a subject (01-design-spec-v0.4.md section 1.2 principle
// 2), with intra-page supersession expressed as keep/reword/retire/add.
package compile
