// Package embed wraps github.com/rostamlabs/rembed to add an optional, in-process
// semantic-embedding signal to search, without ever making search depend on it.
//
// Model weights are never bundled into the balise binary and never downloaded on a plain
// build or on a plain "balise serve"/"balise mcp --stdio" with no configuration: TryLoad
// only attempts anything at all when BALISE_EMBED_MODEL is set, so the very first network
// call (if the model is not already in rembed's local cache) only ever happens because an
// operator explicitly asked for it, never as a surprise side effect of running the CLI.
// When no model is configured, or when loading one fails for any reason (offline, typo'd
// model name, corrupt cache), TryLoad reports that back but never panics and the caller is
// expected to fall back to lexical-only search with no user-visible error -- semantic
// search is additive, not load-bearing.
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/rostamlabs/rembed"
)

// ModelEnv names the environment variable that opts into semantic embedding. Its value is
// passed straight to rembed.Load: a local directory, a bare Hugging Face model id such as
// "sentence-transformers/all-MiniLM-L6-v2", or an "hf:org/name" reference. Leaving it unset
// (the default) disables semantic search entirely, with no attempt to reach the network.
const ModelEnv = "BALISE_EMBED_MODEL"

// DimEnv optionally truncates the model's native dimensionality via rembed's Matryoshka
// support (rembed.WithDim). Leaving it unset keeps the model's full native dimension.
const DimEnv = "BALISE_EMBED_DIM"

// Embedder is a loaded embedding model, ready to embed claim text.
type Embedder struct {
	inner *rembed.Embedder
	model string
}

// TryLoad reads ModelEnv and, only if it is set, loads that model via rembed. It returns
// (nil, nil) when semantic search is not configured at all -- the expected, silent,
// no-network-call default -- and (nil, err) when a model was requested but could not be
// loaded, so the caller can log a diagnostic and continue without one. It never returns a
// non-nil Embedder alongside a non-nil error.
func TryLoad() (*Embedder, error) {
	model := strings.TrimSpace(os.Getenv(ModelEnv))
	if model == "" {
		return nil, nil
	}

	var opts []rembed.Option
	if raw := strings.TrimSpace(os.Getenv(DimEnv)); raw != "" {
		d, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s=%q: %w", DimEnv, raw, err)
		}
		opts = append(opts, rembed.WithDim(d))
	}

	inner, err := rembed.Load(model, opts...)
	if err != nil {
		return nil, fmt.Errorf("load embedding model %q: %w", model, err)
	}
	return &Embedder{inner: inner, model: model}, nil
}

// Model returns the model reference this Embedder was loaded with -- the same string
// stored alongside every vector it produces, so a later model switch cannot be mistaken
// for a cache hit against vectors from a different embedding space.
func (e *Embedder) Model() string {
	if e == nil {
		return ""
	}
	return e.model
}

// Dim returns the length of every vector this Embedder produces.
func (e *Embedder) Dim() int {
	if e == nil {
		return 0
	}
	return e.inner.Dim()
}

// Embed returns one L2-normalised vector per text, in order.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("embed: nil embedder")
	}
	return e.inner.Embed(ctx, texts)
}

// Close releases any resources the underlying model holds. It is safe to call on a nil
// Embedder (the "no model configured" case) and is a no-op unless the model was loaded
// with on-disk weights.
func (e *Embedder) Close() error {
	if e == nil {
		return nil
	}
	return e.inner.Close()
}

// Hash returns the content-address of a claim's text: the SHA-256 hex digest of its
// normalised form. It is paired with a model name as the cache key for claim_embeddings
// (internal/store/migrations/00007_claim_embeddings.sql) -- identical text under the same
// model always hashes identically, so re-embedding is skipped whenever neither the claim's
// wording nor the configured model has changed since the last indexing pass.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(normalize(text)))
	return hex.EncodeToString(sum[:])
}

// normalize collapses whitespace so that cosmetic differences (a trailing space, a
// reformatted line wrap) don't defeat the cache; it intentionally does not lowercase or
// stem, since that is the embedding model's job, not the cache key's.
func normalize(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// EncodeVector packs a []float32 into little-endian bytes for storage in the
// claim_embeddings.vector bytea column.
func EncodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeVector is the inverse of EncodeVector.
func DecodeVector(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("decode vector: length %d is not a multiple of 4", len(buf))
	}
	v := make([]float32, len(buf)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return v, nil
}

// Cosine computes cosine similarity between two vectors. It does not assume its inputs are
// already unit-length (rembed's own output is, but this stays correct even if that ever
// changes upstream), and returns 0 for a zero-length vector or a dimension mismatch rather
// than dividing by zero or panicking on an out-of-range index.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
