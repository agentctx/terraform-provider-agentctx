// Package deployid implements deployment ID generation per spec §2.5.
//
// Format: dep_<timestamp>_<random>
//   - timestamp: UTC YYYYMMDD'T'HHmmss'Z'
//   - random:    8 lowercase hex characters from crypto/rand
//
// Example: dep_20260213T200102Z_6f2c9a1b
package deployid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	prefix       = "dep_"
	timestampFmt = "20060102T150405Z"
	randomBytes  = 4 // 4 bytes = 8 hex chars
)

// New generates a new deployment ID using the current UTC time and
// cryptographically random bytes.
func New() string {
	ts := time.Now().UTC().Format(timestampFmt)

	b := make([]byte, randomBytes)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("deployid: crypto/rand failed: %v", err))
	}

	return prefix + ts + "_" + hex.EncodeToString(b)
}

// Parse extracts the timestamp from a deployment ID string. It returns
// an error if the ID is not in the expected format.
func Parse(id string) (time.Time, error) {
	if !strings.HasPrefix(id, prefix) {
		return time.Time{}, fmt.Errorf("deployid: invalid prefix in %q", id)
	}

	rest := id[len(prefix):]
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("deployid: missing random segment in %q", id)
	}

	ts, err := time.Parse(timestampFmt, parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("deployid: bad timestamp in %q: %w", id, err)
	}

	// Validate the random portion: must be exactly 8 hex characters.
	if len(parts[1]) != randomBytes*2 {
		return time.Time{}, fmt.Errorf("deployid: random segment wrong length in %q", id)
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return time.Time{}, fmt.Errorf("deployid: random segment not hex in %q: %w", id, err)
	}

	return ts, nil
}

// IsValid reports whether id is a well-formed deployment ID.
func IsValid(id string) bool {
	_, err := Parse(id)
	return err == nil
}
