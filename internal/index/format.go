// Package index stores vectors in a single binary file with the following
// little-endian layout, written with encoding/binary for the header and a
// single encoding/gob record for the payload.
//
// The header has two accepted versions. Version 1 (LegacyFileVersion) is the
// original layout and has no build signature. Version 2 (FileVersion) appends
// one length-prefixed build-signature string after the root. Readers accept
// both versions; in each case the gob payload immediately follows the last
// header field.
//
//	version 1 (LegacyFileVersion):
//
//	offset  size   field
//	------  -----  -------------------------------------------------------
//	0       6      magic: the exact bytes "KBCLI\x00"
//	6       4      uint32 file version (== LegacyFileVersion)
//	10      4      uint32 vector width (dim)
//	14      4      uint32 byte length of the embedder version string
//	18      len    UTF-8 embedder version bytes
//	..      4      uint32 byte length of the root string
//	..      len    UTF-8 root bytes
//	..      ..     one encoding/gob record: diskPayload
//
//	version 2 (FileVersion): identical to version 1 plus one field inserted
//	immediately before the gob payload. The signature's length prefix occupies
//	the 4 bytes right after the root bytes, shifting the payload start.
//
//	..      4      uint32 byte length of the build signature string
//	..      len    UTF-8 build signature bytes
//	..      ..     one encoding/gob record: diskPayload
//
// The embedder version is the retrieval-domain key. It is kept in the header
// (not the gob record) so it can be validated before any payload is decoded;
// vectors from different versions must never be compared by distance.
// diskPayload has exported fields so encoding/gob can see the chunk vectors
// ([]float32) and the tracked file states. Parsing and validating the header
// first means a truncated or foreign file is rejected without ever allocating
// memory for the gob payload.
package index

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
)

const (
	magicLen = len(FileMagic)
	// maxHeaderString bounds the length-prefixed header strings so a corrupt
	// length field cannot force a multi-gigabyte allocation during read.
	maxHeaderString = 1 << 20
)

// diskPayload is the one gob record that follows the header. Fields are
// exported so gob can serialize them.
type diskPayload struct {
	Chunks  []Chunk
	Sources []FileState
}

// fileHeader is the fixed, length-prefixed prefix of every vector file.
// BuildSignature is serialized only for version 2 and newer.
type fileHeader struct {
	Version         uint32
	Dim             uint32
	EmbedderVersion string
	Root            string
	BuildSignature  string
}

// writeHeader writes the fixed header to w using little-endian integers.
func writeHeader(w io.Writer, h fileHeader) error {
	if _, err := io.WriteString(w, FileMagic); err != nil {
		return fmt.Errorf("index: write magic: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, h.Version); err != nil {
		return fmt.Errorf("index: write version: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, h.Dim); err != nil {
		return fmt.Errorf("index: write dim: %w", err)
	}
	if err := writeLPString(w, h.EmbedderVersion); err != nil {
		return fmt.Errorf("index: write embedder version: %w", err)
	}
	if err := writeLPString(w, h.Root); err != nil {
		return fmt.Errorf("index: write root: %w", err)
	}
	// Version 2 and newer append the build signature after the root. Version
	// 1 files must omit this field so the gob payload follows the root bytes
	// directly (that is the legacy layout).
	if h.Version >= FileVersion {
		if err := writeLPString(w, h.BuildSignature); err != nil {
			return fmt.Errorf("index: write build signature: %w", err)
		}
	}
	return nil
}

// readHeader parses and validates the fixed header. The gob decoder is only
// created after this succeeds, so structural corruption is reported before
// any payload memory is allocated. It accepts both the legacy version-1
// layout (no signature) and the current version-2 layout (with signature).
func readHeader(r io.Reader) (fileHeader, error) {
	var h fileHeader

	magic := make([]byte, magicLen)
	if _, err := io.ReadFull(r, magic); err != nil {
		return h, fmt.Errorf("index: read magic (truncated or empty file): %w", err)
	}
	if !bytes.Equal(magic, []byte(FileMagic)) {
		return h, fmt.Errorf("index: bad magic %q: not a kbcli vector file", magic)
	}
	if err := binary.Read(r, binary.LittleEndian, &h.Version); err != nil {
		return h, fmt.Errorf("index: read version (truncated header): %w", err)
	}
	if h.Version != LegacyFileVersion && h.Version != FileVersion {
		return h, fmt.Errorf("index: unsupported file version %d (want %d or %d)", h.Version, LegacyFileVersion, FileVersion)
	}
	if err := binary.Read(r, binary.LittleEndian, &h.Dim); err != nil {
		return h, fmt.Errorf("index: read dim (truncated header): %w", err)
	}

	ev, err := readLPString(r)
	if err != nil {
		return h, fmt.Errorf("index: read embedder version: %w", err)
	}
	h.EmbedderVersion = ev
	root, err := readLPString(r)
	if err != nil {
		return h, fmt.Errorf("index: read root: %w", err)
	}
	h.Root = root

	// Only version 2 and newer carry a build signature. For version 1 we must
	// NOT consume another length-prefixed string: the gob payload begins right
	// here, and h.BuildSignature stays empty.
	if h.Version >= FileVersion {
		sig, err := readLPString(r)
		if err != nil {
			return h, fmt.Errorf("index: read build signature: %w", err)
		}
		h.BuildSignature = sig
	}
	return h, nil
}

// writeLPString writes a uint32 length followed by that many UTF-8 bytes.
func writeLPString(w io.Writer, s string) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

// readLPString reads a length-prefixed string, rejecting implausible lengths
// that would otherwise be a denial-of-service on corrupt input.
func readLPString(r io.Reader) (string, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", fmt.Errorf("index: read string length (truncated header): %w", err)
	}
	if n > maxHeaderString {
		return "", fmt.Errorf("index: header string length %d exceeds limit %d (corrupt file)", n, maxHeaderString)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("index: read string (truncated header): %w", err)
	}
	return string(buf), nil
}

// encodePayload writes the single gob record holding chunks and source states.
func encodePayload(w io.Writer, p diskPayload) error {
	if err := gob.NewEncoder(w).Encode(p); err != nil {
		return fmt.Errorf("index: encode gob payload: %w", err)
	}
	return nil
}

// decodePayload reads the single gob record that follows the header.
func decodePayload(r io.Reader) (diskPayload, error) {
	var p diskPayload
	if err := gob.NewDecoder(r).Decode(&p); err != nil {
		return p, fmt.Errorf("index: decode gob payload: %w", err)
	}
	return p, nil
}

// writeVectorFile writes the header followed by the single gob record.
func writeVectorFile(w io.Writer, h fileHeader, p diskPayload) error {
	if err := writeHeader(w, h); err != nil {
		return err
	}
	return encodePayload(w, p)
}
