package subagent

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestComputeHash(t *testing.T) {
	content := "hello world"
	hash := computeHash(content)

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected hash to start with 'sha256:', got %q", hash)
	}

	// SHA-256 hex digest is 64 characters.
	hexPart := strings.TrimPrefix(hash, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("expected 64 hex characters after prefix, got %d", len(hexPart))
	}

	// Same input should produce the same hash.
	hash2 := computeHash(content)
	if hash != hash2 {
		t.Errorf("expected deterministic hashing, got %q and %q", hash, hash2)
	}

	// Different input should produce a different hash.
	hash3 := computeHash("different content")
	if hash == hash3 {
		t.Errorf("expected different hashes for different content")
	}
}

func TestConvertHookMatchers(t *testing.T) {
	tests := []struct {
		name     string
		matchers []HookMatcherModel
		wantLen  int
	}{
		{
			name:     "empty",
			matchers: nil,
			wantLen:  0,
		},
		{
			name: "single matcher with hooks",
			matchers: []HookMatcherModel{
				{
					Matcher: stringValue("Bash"),
					Hooks: []HookEntryModel{
						{Type: stringValue("command"), Command: stringValue("./validate.sh")},
					},
				},
			},
			wantLen: 1,
		},
		{
			name: "multiple matchers",
			matchers: []HookMatcherModel{
				{
					Matcher: stringValue("Bash"),
					Hooks: []HookEntryModel{
						{Type: stringValue("command"), Command: stringValue("./validate.sh")},
					},
				},
				{
					Hooks: []HookEntryModel{
						{Type: stringValue("command"), Command: stringValue("./cleanup.sh")},
					},
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHookMatchers(tt.matchers)
			if len(result) != tt.wantLen {
				t.Errorf("expected %d matchers, got %d", tt.wantLen, len(result))
			}
		})
	}
}

// stringValue returns a types.String with the given value.
// This is a test helper that creates a known-value string.
func stringValue(s string) types.String {
	return types.StringValue(s)
}
