// Package indexer turns a page on disk into rows in Postgres.
package indexer

import (
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// DefaultMaxChunkTokens is 04 section 7 step 6's ceiling.
const DefaultMaxChunkTokens = 400

// Chunk is one searchable slice of a page body.
type Chunk struct {
	Ord         int
	HeadingPath string
	Text        string
	Tokens      int
}

var (
	encoderOnce sync.Once
	encoder     *tiktoken.Tiktoken
)

// CountTokens counts with cl100k_base, the reference tokenizer from 04 section 20.
// The BPE ranks are loaded offline so the binary never reaches the network — spec
// section 3.1 requires offline-capable.
func CountTokens(text string) int {
	if text == "" {
		return 0
	}
	encoderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
		enc, err := tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			encoder = nil
			return
		}
		encoder = enc
	})
	if encoder == nil {
		// Documented fallback: roughly four characters per token. Only reachable if
		// the embedded ranks fail to load, which the tests would catch.
		return (len(text) + 3) / 4
	}
	return len(encoder.Encode(text, nil, nil))
}

// ChunkBody splits a body at headings, then by size, never inside a fence or a table.
func ChunkBody(body string, maxTokens int) []Chunk {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxChunkTokens
	}
	var chunks []Chunk
	for _, section := range splitByHeading(body) {
		for _, piece := range splitBySize(section.text, maxTokens) {
			trimmed := strings.TrimSpace(piece)
			if trimmed == "" {
				continue
			}
			chunks = append(chunks, Chunk{
				Ord:         len(chunks),
				HeadingPath: section.path,
				Text:        trimmed,
				Tokens:      CountTokens(trimmed),
			})
		}
	}
	if len(chunks) == 0 {
		trimmed := strings.TrimSpace(body)
		return []Chunk{{Ord: 0, Text: trimmed, Tokens: CountTokens(trimmed)}}
	}
	return chunks
}

type section struct {
	path string
	text string
}

func splitByHeading(body string) []section {
	sections := []section{{}}
	var stack []string
	inFence := false

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		level, title := headingOf(line)
		if !inFence && level > 0 {
			if level-2 < len(stack) {
				stack = stack[:level-2]
			}
			stack = append(stack, title)
			sections = append(sections, section{path: strings.Join(stack, " > ")})
			continue
		}
		sections[len(sections)-1].text += line + "\n"
	}
	return sections
}

func headingOf(line string) (int, string) {
	if !strings.HasPrefix(line, "##") {
		return 0, ""
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level < 2 || level > 3 || level >= len(line) || line[level] != ' ' {
		return 0, ""
	}
	return level, strings.TrimSpace(line[level:])
}

// splitBySize packs blocks up to maxTokens. Atomic blocks (fences, tables) are never
// broken even when oversized; a long run of prose with no paragraph break is still
// subject to the maxTokens ceiling, so it is packed at word granularity instead.
func splitBySize(text string, maxTokens int) []string {
	var pieces []string
	var current []string
	running := 0

	flush := func() {
		if len(current) > 0 {
			pieces = append(pieces, strings.Join(current, "\n\n"))
			current, running = nil, 0
		}
	}

	for _, block := range atomicBlocks(text) {
		for _, piece := range splitOversized(block, maxTokens) {
			size := CountTokens(piece)
			if len(current) > 0 && running+size > maxTokens {
				flush()
			}
			current = append(current, piece)
			running += size
		}
	}
	flush()
	return pieces
}

// splitOversized returns a block unchanged when it is atomic (a fence or a table) or
// already within maxTokens. Otherwise — a paragraph of prose with no internal blank
// line to split on — it repacks the block word by word, so long prose still obeys the
// size ceiling instead of forming one unbounded chunk.
func splitOversized(block textBlock, maxTokens int) []string {
	if block.atomic || CountTokens(block.text) <= maxTokens {
		return []string{block.text}
	}
	var pieces []string
	var current []string
	running := 0
	for _, word := range strings.Fields(block.text) {
		size := CountTokens(word)
		if len(current) > 0 && running+size > maxTokens {
			pieces = append(pieces, strings.Join(current, " "))
			current, running = nil, 0
		}
		current = append(current, word)
		running += size
	}
	if len(current) > 0 {
		pieces = append(pieces, strings.Join(current, " "))
	}
	return pieces
}

// textBlock is one paragraph, fence, or table produced by atomicBlocks. Atomic blocks
// must never be split further, however large; non-atomic blocks may still be split by
// size when they alone exceed maxTokens.
type textBlock struct {
	text   string
	atomic bool
}

// atomicBlocks splits on blank lines but never inside a fence or a run of table rows.
// Fences and tables are marked atomic; ordinary prose paragraphs are not.
func atomicBlocks(text string) []textBlock {
	var blocks []textBlock
	var current []string
	inFence, inTable := false, false

	flush := func() {
		if len(current) > 0 {
			blocks = append(blocks, textBlock{
				text:   strings.Join(current, "\n"),
				atomic: inFence || inTable,
			})
			current = nil
		}
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			current = append(current, line)
			if inFence {
				flush()
			}
			inFence = !inFence
			continue
		}
		if inFence {
			current = append(current, line)
			continue
		}
		isRow := strings.HasPrefix(strings.TrimSpace(line), "|")
		switch {
		case isRow && !inTable:
			flush()
			inTable = true
		case inTable && !isRow:
			flush()
			inTable = false
		}
		if strings.TrimSpace(line) == "" && !inTable {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()

	var out []textBlock
	for _, block := range blocks {
		if strings.TrimSpace(block.text) != "" {
			out = append(out, block)
		}
	}
	return out
}
