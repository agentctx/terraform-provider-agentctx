package bundle

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

// FileEntry represents a single file discovered during enumeration.
type FileEntry struct {
	RelPath string // forward-slash relative path from source_dir root
	AbsPath string // absolute filesystem path
}

// EnumerateFiles walks sourceDir, excludes files and directories per the
// hardcoded and user-supplied rules, and returns a deterministically sorted
// list of file entries.
func EnumerateFiles(sourceDir string, userExcludes []string) ([]FileEntry, error) {
	absRoot, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("bundle: resolve source dir: %w", err)
	}

	var entries []FileEntry

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return fmt.Errorf("bundle: compute relative path: %w", err)
		}

		// Normalise to forward slashes for the exclusion logic.
		rel = filepath.ToSlash(rel)

		// Skip the root itself.
		if rel == "." {
			return nil
		}

		// For directories, check if the entire subtree should be skipped.
		if d.IsDir() {
			if ShouldExclude(rel, userExcludes) || ShouldExclude(rel+"/", userExcludes) {
				return fs.SkipDir
			}
			return nil
		}

		// Regular file (or symlink to file) — check exclusion.
		if ShouldExclude(rel, userExcludes) {
			return nil
		}

		entries = append(entries, FileEntry{
			RelPath: rel,
			AbsPath: path,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: walk source dir: %w", err)
	}

	// Sort lexicographically by relative path (byte order) for determinism.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelPath < entries[j].RelPath
	})

	return entries, nil
}
