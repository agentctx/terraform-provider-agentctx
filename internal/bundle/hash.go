package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const hashPrefix = "sha256:"

// ComputeFileHash reads the file at path and returns its SHA-256 hash in
// the canonical format "sha256:<hex>".
func ComputeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("bundle: open for hash: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("bundle: read for hash: %w", err)
	}

	return hashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeFileHashBytes computes the SHA-256 hash of an in-memory byte slice
// and returns it in the canonical format "sha256:<hex>".
func ComputeFileHashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hashPrefix + hex.EncodeToString(h[:])
}

// ComputeBundleHash computes a deterministic SHA-256 hash over a set of
// file hashes per spec §2.4.
//
// It takes a map of relpath -> "sha256:<hex>", sorts the keys
// lexicographically, builds entry strings "<relpath>\0<hex>\n", and hashes
// the concatenation. Returns "sha256:<hex>".
func ComputeBundleHash(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		hashHex := files[k]
		// Strip the "sha256:" prefix to get just the hex digest.
		hexPart := strings.TrimPrefix(hashHex, hashPrefix)
		entry := k + "\x00" + hexPart + "\n"
		h.Write([]byte(entry))
	}

	return hashPrefix + hex.EncodeToString(h.Sum(nil))
}

// HashFiles computes per-file SHA-256 hashes and the overall bundle hash
// for the given file entries rooted at sourceDir.
func HashFiles(sourceDir string, files []FileEntry) (fileHashes map[string]string, bundleHash string, err error) {
	fileHashes = make(map[string]string, len(files))

	for _, f := range files {
		hash, err := ComputeFileHash(f.AbsPath)
		if err != nil {
			return nil, "", fmt.Errorf("bundle: hash file %q: %w", f.RelPath, err)
		}
		fileHashes[f.RelPath] = hash
	}

	bundleHash = ComputeBundleHash(fileHashes)
	return fileHashes, bundleHash, nil
}
