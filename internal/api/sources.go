package api

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/vault"
)

// This file is the Sources screen's backend (knowledge-v3.html lines 397-440): a real write
// path that lets a person drop a markdown file or paste text into the vault without the CLI,
// plus the ingest log that shows what has actually landed. "Connected sources" — the third
// piece of that screen — has no backend here at all, on purpose: there is no connector
// framework, no connectors table and no sync jobs in this build (deferred per 04), so that
// section is rendered entirely client-side as one honest empty state rather than invented
// rows this file would have to fabricate.

// sourcesMaxBodyBytes caps a dropped/pasted page's raw content at 2 MiB — generous for any
// markdown or plain-text note a person would paste or drag in, small enough that an
// accidental image/video/binary drop is rejected outright rather than silently truncated or
// committed to git history as a multi-megabyte blob.
const sourcesMaxBodyBytes = 2 << 20 // 2 MiB

// sourcesUploadResponse is POST /api/sources/pages's success body.
type sourcesUploadResponse struct {
	Commit     string `json:"commit"`
	Scope      string `json:"scope"`
	Slug       string `json:"slug"`
	Path       string `json:"path"`
	Title      string `json:"title"`
	IndexStale bool   `json:"index_stale"`
}

// sourceMeta is the frontmatter this handler writes for an uploaded page. It deliberately
// carries fewer fields than internal/importer's Meta (no aliases, identity, source_file —
// none of those exist for a page that arrived by drop/paste, and inventing values for them
// would be exactly the kind of fabrication the task brief rules out). indexer/pipeline.go's
// frontmatter struct treats every one of these as optional with a safe fallback, so a page
// missing a field this struct doesn't set (e.g. owner) still indexes cleanly — it is simply
// not claimed to have one.
type sourceMeta struct {
	UID    string   `yaml:"uid"`
	Slug   string   `yaml:"slug"`
	Type   string   `yaml:"type"`
	Scope  string   `yaml:"scope"`
	Title  string   `yaml:"title"`
	Status string   `yaml:"status"`
	Tags   []string `yaml:"tags"`
}

// handleSourcesUpload is the drop-zone's write path: POST /api/sources/pages?scope=&slug=&title=
// with the page's raw markdown/text content as the request body. This is a security boundary
// — a browser-triggered write into a git repo — so every input is validated server-side; none
// of scope, slug or content is trusted from the request alone.
func (s *server) handleSourcesUpload(w http.ResponseWriter, r *http.Request) {
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	title := strings.TrimSpace(r.URL.Query().Get("title"))

	// Structural check first (no path separators, no "..", no control characters) via the
	// one validator every other caller-supplied path segment in this codebase already uses
	// — internal/vault.ValidateSegment, shared with internal/mcp's remember and
	// internal/store's CreateToken. Do not add a second one here.
	if err := vault.ValidateSegment(scope); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("scope: %s", err))
		return
	}
	if err := vault.ValidateSegment(slug); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("slug: %s", err))
		return
	}
	// Membership check second: a structurally valid scope is not necessarily one of this
	// vault's actual scopes. Checked against store.Queries.AllowedScopes(), the same
	// boundary home.go's homeChanges/homeWaiting already enforce for filesystem-backed
	// reads — never trusted from the request body/query alone.
	if !scopeAllowed(s.q().AllowedScopes(), scope) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("scope %q is not one of this vault's scopes", scope))
		return
	}

	pagePath, err := sourcePagePath(scope, slug)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, sourcesMaxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read request body: "+err.Error())
		return
	}
	if len(body) > sourcesMaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("content exceeds the %d byte limit", sourcesMaxBodyBytes))
		return
	}
	content := string(body)
	if strings.TrimSpace(content) == "" {
		writeError(w, http.StatusBadRequest, "content must not be empty")
		return
	}
	// Markdown and plain text only — knowledge-v3.html's original copy mentioned PDFs,
	// images and recordings "kept as attachments," but there is no blob store in this
	// build, so anything that is not valid UTF-8 text (a null byte included) is refused
	// outright with a clear reason rather than accepted and silently losing the file's
	// actual content.
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		writeError(w, http.StatusBadRequest, "only markdown and plain text are supported; this file looks like binary content")
		return
	}

	// Existing-slug conflict: scope-wide, not just this one path, since a page's real slug
	// comes from its frontmatter (or filename) and could already live at a different path
	// within the same scope. findPageBySlug (review.go) is the existing mechanism for
	// exactly this lookup — reused rather than duplicated. A nil error means the slug is
	// taken; silently overwriting the existing page is not acceptable, so this is a 409,
	// the same status review.go's own optimistic-concurrency check uses for "this changed
	// under you."
	if _, _, _, err := findPageBySlug(s.pages, scope, slug); err == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("a page with slug %q already exists in scope %q", slug, scope))
		return
	}

	if title == "" {
		title = deriveTitle(content, slug)
	}

	rendered, err := renderSourcePage(sourceMeta{
		UID: vault.NewUID(), Slug: slug, Type: "note", Scope: scope, Title: title,
		Status: "active", Tags: []string{},
	}, strings.TrimSpace(content)+"\n")
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	// "web <web@localhost>" — distinct from the importer/compile's shared "balise
	// <balise@localhost>" and from remember's "agent:<name>": there is no authenticated-
	// user or agent-token identity anywhere on this request (no session on the server
	// struct), so the honest author is "a browser session against this API," not a named
	// person or agent. GitPageStore.Write is not used here for the same reason remember.go
	// avoids it — it hardcodes that shared author — only Commit lets a caller supply one.
	author := "web <web@localhost>"
	message := fmt.Sprintf("sources: add %s/%s", scope, slug)
	sha, err := s.pages.Commit([]store.Change{{Path: pagePath, Data: []byte(rendered)}}, author, message)
	if err != nil {
		writeServerError(w, r, err)
		return
	}

	// Same reasoning as review.go's reindexAfterAccept: the commit has already landed by
	// this point, so failing the whole request over a reindex failure would be a lie (the
	// write succeeded) and silently reporting index_stale=false would be a different lie.
	writeJSON(w, http.StatusCreated, sourcesUploadResponse{
		Commit: sha, Scope: scope, Slug: slug, Path: pagePath, Title: title,
		IndexStale: s.reindexAfterAccept(r.Context()),
	})
}

// handleSourcesLog serves the Ingest log: GET /api/sources/log. It is exactly homeChanges —
// the same real commits Home's "Changed recently" block already surfaces (importer commits,
// remember commits, and now this file's own "sources: add ..." commits) — reused rather than
// reimplemented, since "every page arriving in the vault is a commit" is the same problem
// homeChanges already solves via s.pages.List("") + per-path History, merged and deduped by
// SHA. There is no separate "failed" state to report: a git commit that lands is, by
// definition, a success, so fabricating a failure row here would misrepresent real history.
func (s *server) handleSourcesLog(w http.ResponseWriter, r *http.Request) {
	changes, err := s.homeChanges(r.Context())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingests": changes})
}

// sourcePagePath builds the vault-relative path for an uploaded page: <scope>/notes/<slug>.md,
// matching internal/importer/claudememory.go's typeFolders["note"] = "notes" convention and
// defaults/types/note.yaml's folder: notes/ — the same shape a "note" type page gets anywhere
// else in this vault. scope and slug are assumed already run through vault.ValidateSegment by
// the caller; this is the same belt-and-braces second check internal/mcp/path.go's
// rememberPath uses (compose, path.Clean, verify the cleaned result still starts with the
// expected prefix) so a latent bug in ValidateSegment alone could not, by itself, produce a
// path that escapes scope/notes/.
func sourcePagePath(scope, slug string) (string, error) {
	prefix := scope + "/notes/"
	candidate := prefix + slug + ".md"
	cleaned := path.Clean(candidate)
	if cleaned != candidate || !strings.HasPrefix(cleaned, prefix) {
		return "", fmt.Errorf("%w: %q escapes expected prefix %q", vault.ErrInvalidSegment, candidate, prefix)
	}
	return cleaned, nil
}

// renderSourcePage encodes meta as YAML frontmatter and wraps body with the "---" delimiters
// — the identical pattern internal/importer/claudememory.go's renderMeta uses (encode a plain
// Go struct with a yaml.Encoder, not build a vault.Page/yaml.Node by hand), reused here rather
// than re-derived.
func renderSourcePage(meta sourceMeta, body string) (string, error) {
	var sb strings.Builder
	encoder := yaml.NewEncoder(&sb)
	encoder.SetIndent(2)
	if err := encoder.Encode(meta); err != nil {
		return "", fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close encoder: %w", err)
	}
	return "---\n" + sb.String() + "---\n" + body, nil
}

// deriveTitle reads a title from the content itself rather than inventing one: if the first
// non-blank line is a level-1 markdown heading ("# ..."), its text becomes the title exactly
// as written; otherwise the title falls back to the slug, the same default
// indexer/pipeline.go's own orDefault(fm.Title, args.slug) uses for a page with no title.
func deriveTitle(content, slug string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			if t := strings.TrimSpace(rest); t != "" {
				return t
			}
		}
		break
	}
	return slug
}
