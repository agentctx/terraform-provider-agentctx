package bundle

import (
	"fmt"
	"sort"
)

// Bundle is the result of scanning a source directory. It contains the
// enumerated files, their individual hashes, and the overall bundle hash.
type Bundle struct {
	SourceDir  string
	Files      []FileEntry
	FileHashes map[string]string // relpath -> "sha256:<hex>"
	BundleHash string            // "sha256:<hex>"
}

// ScanBundle enumerates files in sourceDir, validates symlinks, computes
// hashes, and returns a fully populated Bundle.
func ScanBundle(sourceDir string, userExcludes []string, allowExternalSymlinks bool) (*Bundle, error) {
	// 1. Enumerate files.
	files, err := EnumerateFiles(sourceDir, userExcludes)
	if err != nil {
		return nil, fmt.Errorf("bundle: enumerate: %w", err)
	}

	// 2. Validate symlinks.
	if err := ValidateSymlinks(sourceDir, files, allowExternalSymlinks); err != nil {
		return nil, fmt.Errorf("bundle: symlinks: %w", err)
	}

	// 3. Hash files.
	fileHashes, bundleHash, err := HashFiles(sourceDir, files)
	if err != nil {
		return nil, fmt.Errorf("bundle: hash: %w", err)
	}

	return &Bundle{
		SourceDir:  sourceDir,
		Files:      files,
		FileHashes: fileHashes,
		BundleHash: bundleHash,
	}, nil
}

// BundleFromFiles creates a Bundle from an in-memory map of relative paths
// to file contents. This is used when deploying downloaded Anthropic bundles
// where no source directory exists on disk.
func BundleFromFiles(files map[string][]byte) *Bundle {
	// Build sorted file entries and compute hashes.
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	entries := make([]FileEntry, 0, len(keys))
	fileHashes := make(map[string]string, len(keys))

	for _, relPath := range keys {
		entries = append(entries, FileEntry{
			RelPath: relPath,
			AbsPath: "", // no on-disk path
		})
		fileHashes[relPath] = ComputeFileHashBytes(files[relPath])
	}

	bundleHash := ComputeBundleHash(fileHashes)

	return &Bundle{
		SourceDir:  "",
		Files:      entries,
		FileHashes: fileHashes,
		BundleHash: bundleHash,
	}
}
