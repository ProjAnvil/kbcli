package index

import (
	"fmt"
	"os"
)

// PeekRoot reads the source directory recorded in a vector file header. It
// does not decode the payload and does not validate the embedder domain, so a
// full rebuild can locate its source even after the configured embedder
// changed. Load would reject such a file by design.
func PeekRoot(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("index: open %s: %w", path, err)
	}
	defer f.Close()

	h, err := readHeader(f)
	if err != nil {
		return "", err
	}
	if h.Root == "" {
		return "", fmt.Errorf("index: %s has an empty root in its header", path)
	}
	return h.Root, nil
}

// PeekBuildSignature reads the build signature recorded in a vector file
// header. It decodes only the header (never the gob payload), so the app can
// report stale index settings without loading any vectors. Legacy version-1
// files have no signature and return an empty string with no error; an
// unsupported version is reported as an error.
func PeekBuildSignature(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("index: open %s: %w", path, err)
	}
	defer f.Close()

	h, err := readHeader(f)
	if err != nil {
		return "", fmt.Errorf("index: read header of %s: %w", path, err)
	}
	return h.BuildSignature, nil
}
