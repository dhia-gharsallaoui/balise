package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dhia/balise/internal/api"
	"github.com/dhia/balise/internal/registry"
	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

// newSourcesServer mirrors newReviewServer: a real defaultsDir so the upload handler's
// post-commit reindex (mirroring review.go's reindexAfterAccept) actually runs, so a
// successful upload's index_stale comes back false rather than true-because-unconfigured.
func newSourcesServer(t *testing.T, scopes store.Scopes) (*httptest.Server, store.PageStore) {
	t.Helper()
	q := store.NewQueries(testutil.NewDB(t), scopes)
	pages := newGitVault(t)
	spaces, err := registry.LoadSpaces("../../defaults/spaces.yaml")
	require.NoError(t, err)
	order, err := registry.LoadOrder("../../defaults/order.yaml")
	require.NoError(t, err)
	server := httptest.NewServer(api.New(q, pages, spaces, order, "../../defaults"))
	t.Cleanup(server.Close)
	return server, pages
}

func postSource(t *testing.T, server *httptest.Server, scope, slug, title, body string) *http.Response {
	t.Helper()
	q := url.Values{}
	if scope != "" {
		q.Set("scope", scope)
	}
	if slug != "" {
		q.Set("slug", slug)
	}
	if title != "" {
		q.Set("title", title)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/sources/pages?"+q.Encode(), strings.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestSourcesUploadHappyPath(t *testing.T) {
	server, pages := newSourcesServer(t, store.Scopes{"work", "client-globex"})

	resp := postSource(t, server, "work", "my-new-note", "", "# My New Note\n\nSome real content about a thing.\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Commit     string `json:"commit"`
		Scope      string `json:"scope"`
		Slug       string `json:"slug"`
		Path       string `json:"path"`
		Title      string `json:"title"`
		IndexStale bool   `json:"index_stale"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.Commit)
	require.Equal(t, "work", out.Scope)
	require.Equal(t, "my-new-note", out.Slug)
	require.Equal(t, "work/notes/my-new-note.md", out.Path)
	require.Equal(t, "My New Note", out.Title, "title should be read from the first '# ' heading")
	require.False(t, out.IndexStale)

	// It really landed as a git commit, on the real page-store commit path — not a second,
	// invented way to write pages.
	raw, _, err := pages.Read("work/notes/my-new-note.md")
	require.NoError(t, err)
	require.Contains(t, string(raw), "Some real content about a thing.")
	require.Contains(t, string(raw), "uid:")
	require.Contains(t, string(raw), "type: note")

	// And it appears in the Home/Sources ingest log, which is real git history, not a
	// fabricated feed.
	logResp, err := http.Get(server.URL + "/api/sources/log")
	require.NoError(t, err)
	defer logResp.Body.Close()
	require.Equal(t, http.StatusOK, logResp.StatusCode)
	var logBody struct {
		Ingests []struct {
			SHA     string `json:"sha"`
			Message string `json:"message"`
			Scope   string `json:"scope"`
			Slug    string `json:"slug"`
		} `json:"ingests"`
	}
	require.NoError(t, json.NewDecoder(logResp.Body).Decode(&logBody))
	var found bool
	for _, ig := range logBody.Ingests {
		if ig.SHA == out.Commit {
			found = true
			require.Equal(t, "work", ig.Scope)
			require.Equal(t, "my-new-note", ig.Slug)
			require.Equal(t, "sources: add work/my-new-note", ig.Message)
		}
	}
	require.True(t, found, "the new commit should appear in the ingest log")
}

func TestSourcesUploadTitleDefaultsToSlugWithoutHeading(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	resp := postSource(t, server, "work", "plain-text-drop", "", "Just a plain sentence, no heading.\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var out struct {
		Title string `json:"title"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, "plain-text-drop", out.Title)
}

func TestSourcesUploadRejectsEmptyBody(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	resp := postSource(t, server, "work", "empty-drop", "", "   \n\t  ")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSourcesUploadRejectsOversizedBody(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	huge := strings.Repeat("a", (2<<20)+1)
	resp := postSource(t, server, "work", "huge-drop", "", huge)
	defer resp.Body.Close()
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestSourcesUploadRejectsBinaryPayload(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	// PNG magic bytes plus a NUL byte and invalid UTF-8 — nothing a markdown/plain-text
	// editor would ever produce.
	binary := string([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0xFF, 0xFE})
	resp := postSource(t, server, "work", "binary-drop", "", binary)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSourcesUploadRejectsPathTraversalInSlug(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	resp := postSource(t, server, "work", "../../etc/passwd", "", "hostile content\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSourcesUploadRejectsDotDotSlug(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	resp := postSource(t, server, "work", "..", "", "hostile content\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSourcesUploadRejectsOutOfVaultScope(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	resp := postSource(t, server, "../../etc", "some-slug", "", "hostile content\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSourcesUploadRejectsUnknownScope(t *testing.T) {
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	resp := postSource(t, server, "not-a-real-scope", "some-slug", "", "real content\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSourcesUploadConflictsOnExistingSlug(t *testing.T) {
	server, pages := newSourcesServer(t, store.Scopes{"work"})
	_, err := pages.Write("work/notes/dup.md", []byte("---\nuid: 01J0000000000000000000000\nslug: dup\ntype: note\nscope: work\ntitle: Dup\n---\nalready here\n"), "")
	require.NoError(t, err)

	resp := postSource(t, server, "work", "dup", "", "trying to overwrite\n")
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	// The original page must be untouched — silently overwriting is not acceptable.
	raw, _, err := pages.Read("work/notes/dup.md")
	require.NoError(t, err)
	require.Contains(t, string(raw), "already here")
	require.NotContains(t, string(raw), "trying to overwrite")
}

func TestSourcesUploadRequiresContentType(t *testing.T) {
	// Belt-and-braces sanity check: a well-formed markdown drop with an unrelated
	// Content-Type header must still succeed — the endpoint sniffs the body, it does not
	// trust the header.
	server, _ := newSourcesServer(t, store.Scopes{"work"})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/sources/pages?scope=work&slug=hdr-test", server.URL), bytes.NewBufferString("# Header Test\nbody\n"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}
