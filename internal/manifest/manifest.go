// Package manifest implements the Manifest v2 struct per spec §6.2 and
// provides deterministic serialization / deserialization.
package manifest

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Manifest is the v2 manifest written alongside every deployment.
type Manifest struct {
	SchemaVersion   int               `json:"schema_version"`
	ProviderVersion string            `json:"provider_version"`
	ResourceType    string            `json:"resource_type"`
	ResourceName    string            `json:"resource_name"`
	CanonicalStore  string            `json:"canonical_store"`
	DeploymentID    string            `json:"deployment_id"`
	CreatedAt       string            `json:"created_at"`
	SourceHash      string            `json:"source_hash"`
	BundleHash      string            `json:"bundle_hash"`
	Origin          *ManifestOrigin   `json:"origin,omitempty"`
	Registry        *ManifestRegistry `json:"registry,omitempty"`
	Files           map[string]string `json:"files"`
}

// ManifestOrigin describes how the source was provided.
type ManifestOrigin struct {
	Type      string `json:"type"`
	SourceDir string `json:"source_dir,omitempty"`
}

// ManifestRegistry describes an Anthropic-registry source.
type ManifestRegistry struct {
	Type       string `json:"type"`
	SkillID    string `json:"skill_id,omitempty"`
	Version    string `json:"version,omitempty"`
	BundleHash string `json:"bundle_hash,omitempty"`
}

// deterministicFiles is a helper type that serializes a map[string]string
// with keys in sorted order so the output is deterministic.
type deterministicFiles struct {
	m map[string]string
}

func (d deterministicFiles) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(d.m))
	for k := range d.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build an ordered JSON object manually.
	buf := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		valBytes, err := json.Marshal(d.m[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, keyBytes...)
		buf = append(buf, ':')
		buf = append(buf, valBytes...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// marshalProxy mirrors Manifest but replaces Files with the deterministic
// wrapper so the top-level struct fields stay in declaration order (which
// encoding/json guarantees for structs) while the map is sorted.
type marshalProxy struct {
	SchemaVersion   int                `json:"schema_version"`
	ProviderVersion string             `json:"provider_version"`
	ResourceType    string             `json:"resource_type"`
	ResourceName    string             `json:"resource_name"`
	CanonicalStore  string             `json:"canonical_store"`
	DeploymentID    string             `json:"deployment_id"`
	CreatedAt       string             `json:"created_at"`
	SourceHash      string             `json:"source_hash"`
	BundleHash      string             `json:"bundle_hash"`
	Origin          *ManifestOrigin    `json:"origin,omitempty"`
	Registry        *ManifestRegistry  `json:"registry,omitempty"`
	Files           deterministicFiles `json:"files"`
}

// Marshal serializes a Manifest to deterministic, indented JSON.
// The Files map keys are sorted lexicographically.
func Marshal(m *Manifest) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("manifest: cannot marshal nil manifest")
	}

	proxy := marshalProxy{
		SchemaVersion:   m.SchemaVersion,
		ProviderVersion: m.ProviderVersion,
		ResourceType:    m.ResourceType,
		ResourceName:    m.ResourceName,
		CanonicalStore:  m.CanonicalStore,
		DeploymentID:    m.DeploymentID,
		CreatedAt:       m.CreatedAt,
		SourceHash:      m.SourceHash,
		BundleHash:      m.BundleHash,
		Origin:          m.Origin,
		Registry:        m.Registry,
		Files:           deterministicFiles{m: m.Files},
	}

	return json.MarshalIndent(proxy, "", "  ")
}

// Unmarshal deserializes JSON bytes into a Manifest.
func Unmarshal(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: unmarshal failed: %w", err)
	}
	return &m, nil
}
