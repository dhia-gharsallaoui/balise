package cli

import (
	"context"
	"fmt"

	"github.com/dhia/balise/internal/embed"
	"github.com/dhia/balise/internal/store"
)

// embedBatchSize caps how many claim texts go into a single embedder.Embed call. rembed's
// own batching is already bit-identical to embedding one text at a time (see internal/embed's
// doc comment quoting its README), so this exists only to bound memory and give a reindex of
// a large vault incremental progress rather than one giant call; it is not a correctness
// requirement.
const embedBatchSize = 64

// EmbedClaims embeds every scoped claim (across every scope Reindex's Queries holds, and
// regardless of historical status -- see ScopedClaimTexts) whose (hash, model) pair is not
// already cached, and writes the results to claim_embeddings. It returns how many distinct
// claim texts were newly embedded.
//
// embedder == nil (no BALISE_EMBED_MODEL configured) is not an error: it is the default,
// silent, no-network-call case, and EmbedClaims reports 0 embedded without ever calling
// ScopedClaimTexts or touching the database -- a plain `balise reindex` with no model
// configured does exactly what it did before this function existed.
//
// Claim text is deduplicated by hash before any embedding call: the same boilerplate or
// shared runbook step recurs across many pages and even across scopes, and content-hash
// keying (claim_embeddings' primary key is (hash, model), not (doc_uid, claim)) means every
// occurrence should reuse one vector rather than pay for embedding it once per page.
func EmbedClaims(ctx context.Context, q *store.Queries, embedder *embed.Embedder) (int, error) {
	if embedder == nil {
		return 0, nil
	}

	candidates, err := q.ScopedClaimTexts(ctx, true)
	if err != nil {
		return 0, fmt.Errorf("embed claims: %w", err)
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	textByHash := make(map[string]string, len(candidates))
	for _, c := range candidates {
		textByHash[embed.Hash(c.Claim)] = c.Claim
	}
	hashes := make([]string, 0, len(textByHash))
	for h := range textByHash {
		hashes = append(hashes, h)
	}

	model := embedder.Model()
	cached, err := q.CachedClaimEmbeddings(ctx, model, hashes)
	if err != nil {
		return 0, fmt.Errorf("embed claims: %w", err)
	}

	var missing []string
	for _, h := range hashes {
		if _, ok := cached[h]; !ok {
			missing = append(missing, h)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	dim := embedder.Dim()
	for start := 0; start < len(missing); start += embedBatchSize {
		end := min(start+embedBatchSize, len(missing))
		batch := missing[start:end]

		texts := make([]string, len(batch))
		for i, h := range batch {
			texts[i] = textByHash[h]
		}
		vectors, err := embedder.Embed(ctx, texts)
		if err != nil {
			return 0, fmt.Errorf("embed claims: embed batch: %w", err)
		}

		writes := make([]store.ClaimEmbeddingWrite, len(batch))
		for i, h := range batch {
			writes[i] = store.ClaimEmbeddingWrite{Hash: h, Vector: embed.EncodeVector(vectors[i])}
		}
		if err := q.UpsertClaimEmbeddings(ctx, model, dim, writes); err != nil {
			return 0, fmt.Errorf("embed claims: store batch: %w", err)
		}
	}

	return len(missing), nil
}
