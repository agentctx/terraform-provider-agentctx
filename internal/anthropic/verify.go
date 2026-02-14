package anthropic

import (
	"fmt"
	"strings"

	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
)

// BundleIntegrityError indicates that a downloaded bundle does not match the
// expected hash, as specified in spec section 9.3.
type BundleIntegrityError struct {
	Version        string
	ExpectedHash   string
	ActualHash     string
	FileMismatches []FileMismatch
}

// FileMismatch records a single file whose hash differs between the expected
// and actual bundle contents.
type FileMismatch struct {
	Path         string
	ExpectedHash string
	ActualHash   string
}

// Error implements the error interface.
func (e *BundleIntegrityError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "bundle integrity check failed for version %s: expected hash %s, got %s",
		e.Version, e.ExpectedHash, e.ActualHash)

	if len(e.FileMismatches) > 0 {
		fmt.Fprintf(&b, "; %d file(s) with mismatched hashes:", len(e.FileMismatches))
		for _, m := range e.FileMismatches {
			fmt.Fprintf(&b, "\n  %s: expected %s, got %s", m.Path, m.ExpectedHash, m.ActualHash)
		}
	}

	return b.String()
}

// VerifyBundle checks downloaded bundle integrity by computing SHA-256 hashes
// of all files and comparing the resulting bundle hash against the expected
// value. It uses the same hashing algorithm as the bundle package to ensure
// consistency with local computations.
func VerifyBundle(files map[string][]byte, expectedBundleHash string) error {
	// Compute per-file hashes using the same canonical format as the bundle package.
	fileHashes := make(map[string]string, len(files))
	for path, data := range files {
		fileHashes[path] = bundle.ComputeFileHashBytes(data)
	}

	// Compute the overall bundle hash from the per-file hashes.
	actualBundleHash := bundle.ComputeBundleHash(fileHashes)

	if actualBundleHash == expectedBundleHash {
		return nil
	}

	return &BundleIntegrityError{
		ExpectedHash: expectedBundleHash,
		ActualHash:   actualBundleHash,
	}
}
