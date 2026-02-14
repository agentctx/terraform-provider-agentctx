package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SymlinkEscapeError is returned when a symlink resolves to a target
// outside the source directory and external symlinks are not allowed.
type SymlinkEscapeError struct {
	Path   string // relative path of the symlink inside source_dir
	Target string // resolved absolute target of the symlink
}

func (e *SymlinkEscapeError) Error() string {
	return fmt.Sprintf("bundle: symlink %q resolves to %q which is outside the source directory", e.Path, e.Target)
}

// ValidateSymlinks checks every file in the list. For each symlink it
// resolves the target and ensures it does not escape sourceDir (unless
// allowExternal is true).
func ValidateSymlinks(sourceDir string, files []FileEntry, allowExternal bool) error {
	absRoot, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("bundle: resolve source dir: %w", err)
	}

	// Resolve the root itself in case it is a symlink.
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return fmt.Errorf("bundle: eval symlinks on source dir: %w", err)
	}

	// Ensure the root ends with a separator for prefix comparison.
	rootPrefix := absRoot + string(filepath.Separator)

	for _, f := range files {
		info, err := os.Lstat(f.AbsPath)
		if err != nil {
			return fmt.Errorf("bundle: lstat %q: %w", f.RelPath, err)
		}

		if info.Mode()&os.ModeSymlink == 0 {
			continue // not a symlink
		}

		resolved, err := filepath.EvalSymlinks(f.AbsPath)
		if err != nil {
			return fmt.Errorf("bundle: resolve symlink %q: %w", f.RelPath, err)
		}

		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return fmt.Errorf("bundle: abs path for symlink %q: %w", f.RelPath, err)
		}

		if allowExternal {
			continue
		}

		// The resolved path must be within absRoot.
		if resolved != absRoot && !strings.HasPrefix(resolved, rootPrefix) {
			return &SymlinkEscapeError{
				Path:   f.RelPath,
				Target: resolved,
			}
		}
	}

	return nil
}
