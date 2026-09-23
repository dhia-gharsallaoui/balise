package embed

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTryLoadWithNoModelConfiguredIsANoOp(t *testing.T) {
	t.Setenv(ModelEnv, "")
	e, err := TryLoad()
	require.NoError(t, err)
	require.Nil(t, e)
}

func TestTryLoadInvalidDimIsAnError(t *testing.T) {
	t.Setenv(ModelEnv, "sentence-transformers/all-MiniLM-L6-v2")
	t.Setenv(DimEnv, "not-a-number")
	e, err := TryLoad()
	require.Error(t, err)
	require.Nil(t, e)
}

func TestNilEmbedderMethodsAreSafe(t *testing.T) {
	var e *Embedder
	require.Equal(t, "", e.Model())
	require.Equal(t, 0, e.Dim())
	require.NoError(t, e.Close())
	_, err := e.Embed(nil, []string{"x"})
	require.Error(t, err)
}

func TestHashIsStableAndNormalises(t *testing.T) {
	require.Equal(t, Hash("the apply failed"), Hash("the apply failed"))
	require.Equal(t, Hash("the  apply   failed"), Hash("the apply failed"), "internal whitespace runs should not change the hash")
	require.Equal(t, Hash("  the apply failed  "), Hash("the apply failed"), "leading/trailing whitespace should not change the hash")
	require.NotEqual(t, Hash("the apply failed"), Hash("the apply succeeded"))
}

func TestEncodeDecodeVectorRoundTrips(t *testing.T) {
	v := []float32{0.1, -0.2, 3.5, 0, 1e-8}
	buf := EncodeVector(v)
	require.Len(t, buf, 4*len(v))
	got, err := DecodeVector(buf)
	require.NoError(t, err)
	require.Equal(t, v, got)
}

func TestDecodeVectorRejectsBadLength(t *testing.T) {
	_, err := DecodeVector([]byte{1, 2, 3})
	require.Error(t, err)
}

func TestCosine(t *testing.T) {
	require.InDelta(t, 1.0, Cosine([]float32{1, 0}, []float32{1, 0}), 1e-9)
	require.InDelta(t, 0.0, Cosine([]float32{1, 0}, []float32{0, 1}), 1e-9)
	require.InDelta(t, -1.0, Cosine([]float32{1, 0}, []float32{-1, 0}), 1e-9)
	require.Equal(t, 0.0, Cosine([]float32{1, 0}, []float32{1, 0, 0}), "dimension mismatch returns 0")
	require.Equal(t, 0.0, Cosine([]float32{0, 0}, []float32{1, 0}), "zero vector returns 0")
	require.Equal(t, 0.0, Cosine(nil, nil))
}

func TestMain(m *testing.M) {
	// Guard against a developer's shell already exporting BALISE_EMBED_MODEL from some
	// other project: tests that need it unset must be able to trust that.
	os.Unsetenv(ModelEnv)
	os.Unsetenv(DimEnv)
	os.Exit(m.Run())
}
