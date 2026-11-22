package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/embed"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// chunkMeta carries the per-file context shared by every chunk of a source.
type chunkMeta struct {
	Source       string // path relative to root, slash-separated
	Title        string // base filename without its extension
	Page         int    // 1-based page for PDFs, 0 otherwise
	TokenCounter embed.TokenCounter
	MaxTokens    int
}

// makeChunk builds one index.Chunk. Text is the exact string handed to the
// embedder: Title + "\n" + Heading + "\n" + Body. Keeping this construction in
// one place guarantees every format produces a uniform embedding input.
func makeChunk(meta chunkMeta, ordinal int, heading, body string) index.Chunk {
	return index.Chunk{
		ID:      chunkID(meta.Source, ordinal),
		Text:    meta.Title + "\n" + heading + "\n" + body,
		Body:    body,
		Source:  meta.Source,
		Title:   meta.Title,
		Heading: heading,
		Page:    meta.Page,
		Ordinal: ordinal,
	}
}

// buildChunks turns passages into Chunks with continuous, zero-based ordinals.
// Token capping happens first so ordinals and IDs are assigned only after the
// final passage boundaries are known.
func buildChunks(passages []passage, meta chunkMeta, startOrdinal int) ([]index.Chunk, error) {
	passages, err := capPassagesByTokens(passages, meta, meta.TokenCounter)
	if err != nil {
		return nil, err
	}
	chunks := make([]index.Chunk, 0, len(passages))
	for i, p := range passages {
		chunks = append(chunks, makeChunk(meta, startOrdinal+i, p.heading, p.body))
	}
	return chunks, nil
}

// chunkID is the stable identity of a chunk: the first 16 hex characters of
// sha256(source + "\x00" + ordinal). (source is Chunk.Source.)
func chunkID(source string, ordinal int) string {
	sum := sha256.Sum256([]byte(source + "\x00" + strconv.Itoa(ordinal)))
	return hex.EncodeToString(sum[:])[:16]
}

func titleOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// supports reports whether the extension is handled, case-insensitively.
func supports(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range supportedExtList() {
		if ext == e {
			return true
		}
	}
	return false
}

// chunkFile parses one file and returns its chunks plus its file state. The
// content is read exactly once: the same bytes feed the state hash and the
// parser. PDF and DOCX are binary; every other supported format is UTF-8 text.
func chunkFile(root, path string, opts Options) (Result, error) {
	if filepath.Base(path) == index.DefaultFileName {
		return Result{Chunks: []index.Chunk{}}, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".doc" {
		return Result{}, fmt.Errorf("unsupported format %q: legacy .doc cannot be parsed; please save the file as .docx and retry", ext)
	}
	if !supports(path) {
		return Result{}, fmt.Errorf("unsupported file extension %q: supported formats are %s", ext, strings.Join(supportedExtList(), ", "))
	}

	info, err := os.Stat(path)
	if err != nil {
		return Result{}, fmt.Errorf("stat %q: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read %q: %w", path, err)
	}
	state := stateFromData(root, path, data, info)
	if len(data) == 0 {
		return Result{State: state, Chunks: []index.Chunk{}}, nil
	}

	meta := chunkMeta{
		Source: relPath(root, path),
		Title:  titleOf(path),
	}
	if opts.TokenCounter != nil {
		meta.TokenCounter = opts.TokenCounter
		meta.MaxTokens = opts.TokenCounter.MaxTokens()
	}

	var text string
	if ext != ".pdf" && ext != ".docx" {
		text, err = decodeText(data)
		if err != nil {
			return Result{}, fmt.Errorf("decode %q: %w", path, err)
		}
	}

	src := Source{Root: root, Path: path, Ext: ext, Text: text, Data: data, Meta: meta, Opts: opts}
	var strategy Strategy
	if opts.ChunkStrategy == StrategyFixed {
		strategy = fixedRegistry().For(ext)
	} else {
		strategy = autoRegistry().For(ext)
	}
	chunks, err := strategy.Chunk(src)
	if err != nil {
		return Result{}, fmt.Errorf("chunk %q: %w", path, err)
	}
	return Result{State: state, Chunks: chunks}, nil
}
