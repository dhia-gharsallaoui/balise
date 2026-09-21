package indexer_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dhia/balise/internal/indexer"
	"github.com/stretchr/testify/require"
)

func headingPaths(chunks []indexer.Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.HeadingPath
	}
	return out
}

func TestBodyWithoutHeadingsIsOneChunk(t *testing.T) {
	chunks := indexer.ChunkBody("Just a paragraph.", 400)
	require.Len(t, chunks, 1)
	require.Empty(t, chunks[0].HeadingPath)
}

func TestSplitsAtH2(t *testing.T) {
	chunks := indexer.ChunkBody("Intro.\n\n## Current\n\nA.\n\n## History\n\nB.\n", 400)
	require.Equal(t, []string{"", "Current", "History"}, headingPaths(chunks))
}

func TestH3NestsUnderH2(t *testing.T) {
	chunks := indexer.ChunkBody("## Setup\n\nA.\n\n### Detail\n\nB.\n", 400)
	require.Equal(t, []string{"Setup", "Setup > Detail"}, headingPaths(chunks))
}

func TestCodeBlockIsNeverSplit(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("echo %d", i))
	}
	body := "## Run\n\n```bash\n" + strings.Join(lines, "\n") + "\n```\n"

	var holding []indexer.Chunk
	for _, chunk := range indexer.ChunkBody(body, 50) {
		if strings.Contains(chunk.Text, "echo 0\n") {
			holding = append(holding, chunk)
		}
	}
	require.Len(t, holding, 1)
	require.Contains(t, holding[0].Text, "echo 499")
}

func TestTableIsNeverSplit(t *testing.T) {
	var rows []string
	for i := 0; i < 200; i++ {
		rows = append(rows, fmt.Sprintf("| a%d | b%d |", i, i))
	}
	body := "## T\n\n| a | b |\n|---|---|\n" + strings.Join(rows, "\n") + "\n"

	var holding []indexer.Chunk
	for _, chunk := range indexer.ChunkBody(body, 50) {
		if strings.Contains(chunk.Text, "| a0 |") {
			holding = append(holding, chunk)
		}
	}
	require.Len(t, holding, 1)
	require.Contains(t, holding[0].Text, "| a199 |")
}

func TestLongProseSplitsBySize(t *testing.T) {
	body := "## Long\n\n" + strings.Repeat("word ", 2000)
	require.Greater(t, len(indexer.ChunkBody(body, 100)), 1)
}

func TestChunksAreOrderedFromZero(t *testing.T) {
	chunks := indexer.ChunkBody("## A\n\nx\n\n## B\n\ny\n", 400)
	for i, chunk := range chunks {
		require.Equal(t, i, chunk.Ord)
	}
}

func TestHeadingInsideAFenceIsNotAHeading(t *testing.T) {
	chunks := indexer.ChunkBody("## Real\n\n```md\n## Not a heading\n```\n", 400)
	require.Equal(t, []string{"Real"}, headingPaths(chunks))
}

func TestCountTokensIsMonotonic(t *testing.T) {
	require.Greater(t, indexer.CountTokens("one two three"), indexer.CountTokens("one"))
	require.Equal(t, 0, indexer.CountTokens(""))
}

// TestSingleOversizedWordIsKeptWhole pins splitOversized's behaviour on a word
// that alone exceeds maxTokens: a token-level limit cannot split inside a
// word, so the function must still terminate and return the word intact in
// exactly one piece, rather than looping forever or returning nothing.
func TestSingleOversizedWordIsKeptWhole(t *testing.T) {
	word := strings.Repeat("x", 300)
	body := "## Long\n\n" + word + "\n"

	chunks := indexer.ChunkBody(body, 1)

	require.Len(t, chunks, 1)
	require.Equal(t, word, chunks[0].Text)
}

// TestOversizedBlockPreservesMultibyteRunes exercises splitOversized's
// word-by-word repacking on non-ASCII content: accented Latin (French, per
// 04 §20's corpus) and CJK, whose per-rune and per-character token costs
// differ from ASCII. Rejoining every chunk must reproduce every occurrence
// of every word undamaged, with no invalid UTF-8 introduced by splitting.
func TestOversizedBlockPreservesMultibyteRunes(t *testing.T) {
	words := []string{"café", "élève", "garçon", "日本語", "中文", "テスト"}
	line := strings.Join(words, " ")
	body := "## Multibyte\n\n" + strings.Repeat(line+" ", 50)

	chunks := indexer.ChunkBody(body, 20)
	require.Greater(t, len(chunks), 1, "expected the oversized block to be split into multiple chunks")

	var rejoined strings.Builder
	for _, chunk := range chunks {
		rejoined.WriteString(chunk.Text)
		rejoined.WriteString(" ")
	}
	joined := rejoined.String()

	require.True(t, utf8.ValidString(joined), "rejoined chunks must remain valid UTF-8")
	require.NotContains(t, joined, "�", "no rune should have been corrupted into a replacement character")
	for _, word := range words {
		require.Equal(t, 50, strings.Count(joined, word), "word %q should survive all 50 repetitions intact", word)
	}
}
