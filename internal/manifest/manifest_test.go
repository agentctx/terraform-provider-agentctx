package manifest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleManifest() *Manifest {
	return &Manifest{
		SchemaVersion:   2,
		ProviderVersion: "0.5.0",
		ResourceType:    "agentctx_skill",
		ResourceName:    "my_skill",
		CanonicalStore:  "s3://my-bucket/skills/",
		DeploymentID:    "dep_20260213T200102Z_6f2c9a1b",
		CreatedAt:       "2026-02-13T20:01:02Z",
		SourceHash:      "sha256:abcdef0123456789",
		BundleHash:      "sha256:9876543210fedcba",
		Origin: &ManifestOrigin{
			Type:      "local",
			SourceDir: "/home/user/project/skills/my_skill",
		},
		Registry: &ManifestRegistry{
			Type:       "anthropic",
			SkillID:    "skill-123",
			Version:    "1.0.0",
			BundleHash: "sha256:aabbccdd",
		},
		Files: map[string]string{
			"main.py":         "sha256:1111111111111111",
			"config.yaml":     "sha256:2222222222222222",
			"lib/helpers.py":  "sha256:3333333333333333",
			"lib/utils.py":    "sha256:4444444444444444",
			"assets/logo.png": "sha256:5555555555555555",
		},
	}
}

func TestMarshalUnmarshal(t *testing.T) {
	original := sampleManifest()

	data, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}

	roundTripped, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}

	// Check top-level scalar fields.
	if roundTripped.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", roundTripped.SchemaVersion, original.SchemaVersion)
	}
	if roundTripped.ProviderVersion != original.ProviderVersion {
		t.Errorf("ProviderVersion = %q, want %q", roundTripped.ProviderVersion, original.ProviderVersion)
	}
	if roundTripped.ResourceType != original.ResourceType {
		t.Errorf("ResourceType = %q, want %q", roundTripped.ResourceType, original.ResourceType)
	}
	if roundTripped.ResourceName != original.ResourceName {
		t.Errorf("ResourceName = %q, want %q", roundTripped.ResourceName, original.ResourceName)
	}
	if roundTripped.CanonicalStore != original.CanonicalStore {
		t.Errorf("CanonicalStore = %q, want %q", roundTripped.CanonicalStore, original.CanonicalStore)
	}
	if roundTripped.DeploymentID != original.DeploymentID {
		t.Errorf("DeploymentID = %q, want %q", roundTripped.DeploymentID, original.DeploymentID)
	}
	if roundTripped.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt = %q, want %q", roundTripped.CreatedAt, original.CreatedAt)
	}
	if roundTripped.SourceHash != original.SourceHash {
		t.Errorf("SourceHash = %q, want %q", roundTripped.SourceHash, original.SourceHash)
	}
	if roundTripped.BundleHash != original.BundleHash {
		t.Errorf("BundleHash = %q, want %q", roundTripped.BundleHash, original.BundleHash)
	}

	// Check Origin.
	if roundTripped.Origin == nil {
		t.Fatal("Origin is nil after round-trip")
	}
	if roundTripped.Origin.Type != original.Origin.Type {
		t.Errorf("Origin.Type = %q, want %q", roundTripped.Origin.Type, original.Origin.Type)
	}
	if roundTripped.Origin.SourceDir != original.Origin.SourceDir {
		t.Errorf("Origin.SourceDir = %q, want %q", roundTripped.Origin.SourceDir, original.Origin.SourceDir)
	}

	// Check Registry.
	if roundTripped.Registry == nil {
		t.Fatal("Registry is nil after round-trip")
	}
	if roundTripped.Registry.Type != original.Registry.Type {
		t.Errorf("Registry.Type = %q, want %q", roundTripped.Registry.Type, original.Registry.Type)
	}
	if roundTripped.Registry.SkillID != original.Registry.SkillID {
		t.Errorf("Registry.SkillID = %q, want %q", roundTripped.Registry.SkillID, original.Registry.SkillID)
	}
	if roundTripped.Registry.Version != original.Registry.Version {
		t.Errorf("Registry.Version = %q, want %q", roundTripped.Registry.Version, original.Registry.Version)
	}
	if roundTripped.Registry.BundleHash != original.Registry.BundleHash {
		t.Errorf("Registry.BundleHash = %q, want %q", roundTripped.Registry.BundleHash, original.Registry.BundleHash)
	}

	// Check Files map.
	if len(roundTripped.Files) != len(original.Files) {
		t.Fatalf("Files length = %d, want %d", len(roundTripped.Files), len(original.Files))
	}
	for k, v := range original.Files {
		if got, ok := roundTripped.Files[k]; !ok {
			t.Errorf("Files missing key %q", k)
		} else if got != v {
			t.Errorf("Files[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestMarshalDeterministic(t *testing.T) {
	m := sampleManifest()

	data1, err := Marshal(m)
	if err != nil {
		t.Fatalf("first Marshal() returned error: %v", err)
	}

	data2, err := Marshal(m)
	if err != nil {
		t.Fatalf("second Marshal() returned error: %v", err)
	}

	if !bytes.Equal(data1, data2) {
		t.Errorf("Marshal() produced different output on two calls:\n--- first ---\n%s\n--- second ---\n%s", data1, data2)
	}
}

func TestMarshalSortedFiles(t *testing.T) {
	m := &Manifest{
		SchemaVersion: 2,
		Files: map[string]string{
			"zebra.py":  "sha256:zzzz",
			"alpha.py":  "sha256:aaaa",
			"middle.py": "sha256:mmmm",
		},
	}

	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}

	output := string(data)

	alphaIdx := strings.Index(output, "alpha.py")
	middleIdx := strings.Index(output, "middle.py")
	zebraIdx := strings.Index(output, "zebra.py")

	if alphaIdx == -1 || middleIdx == -1 || zebraIdx == -1 {
		t.Fatalf("one or more file keys not found in output:\n%s", output)
	}

	if !(alphaIdx < middleIdx && middleIdx < zebraIdx) {
		t.Errorf("file keys are not in sorted order: alpha@%d, middle@%d, zebra@%d\noutput:\n%s",
			alphaIdx, middleIdx, zebraIdx, output)
	}

	// Also verify the JSON is valid.
	if !json.Valid(data) {
		t.Errorf("Marshal() output is not valid JSON:\n%s", data)
	}
}

func TestUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "not JSON",
			input: "this is not json",
		},
		{
			name:  "truncated JSON",
			input: `{"schema_version": 2, "files":`,
		},
		{
			name:  "JSON array instead of object",
			input: `[1, 2, 3]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Unmarshal([]byte(tt.input))
			if err == nil {
				t.Errorf("Unmarshal(%q) expected error, got nil", tt.input)
			}
		})
	}
}

func TestMarshalNil(t *testing.T) {
	_, err := Marshal(nil)
	if err == nil {
		t.Fatal("Marshal(nil) expected error, got nil")
	}
}
