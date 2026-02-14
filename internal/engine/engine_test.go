package engine_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
	"github.com/agentctx/terraform-provider-agentctx/internal/engine"
	"github.com/agentctx/terraform-provider-agentctx/internal/manifest"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// newTestEngine creates an Engine with a generous concurrency limit for tests.
func newTestEngine() *engine.Engine {
	return engine.New(semaphore.NewWeighted(10))
}

// createTempBundle creates a temporary directory with the given files and
// returns a scanned Bundle. The files map keys are relative paths and values
// are file contents.
func createTempBundle(t *testing.T, files map[string]string) *bundle.Bundle {
	t.Helper()
	tmpDir := t.TempDir()

	for relPath, content := range files {
		absPath := filepath.Join(tmpDir, filepath.FromSlash(relPath))
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating directory %s: %v", dir, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("writing file %s: %v", absPath, err)
		}
	}

	b, err := bundle.ScanBundle(tmpDir, nil, false)
	if err != nil {
		t.Fatalf("scanning bundle: %v", err)
	}
	return b
}

// defaultDeployInput returns a DeployInput with sensible defaults for testing.
func defaultDeployInput(b *bundle.Bundle) engine.DeployInput {
	return engine.DeployInput{
		SkillName:       "my-skill",
		Bundle:          b,
		CanonicalStore:  "s3://test-bucket",
		ProviderVersion: "0.1.0-test",
		ResourceName:    "test_resource",
		SourceDir:       b.SourceDir,
	}
}

// deployToTarget is a helper that deploys a bundle and returns the result.
func deployToTarget(t *testing.T, eng *engine.Engine, tgt target.Target, input engine.DeployInput) *engine.DeployResult {
	t.Helper()
	result, err := eng.Deploy(context.Background(), tgt, input)
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}
	return result
}

// readObject reads the full content of an object from the target.
func readObject(t *testing.T, tgt target.Target, key string) []byte {
	t.Helper()
	rc, _, err := tgt.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("reading object %q: %v", key, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading object body %q: %v", key, err)
	}
	return data
}

// objectExists checks whether an object exists in the target.
func objectExists(t *testing.T, tgt target.Target, key string) bool {
	t.Helper()
	_, err := tgt.Head(context.Background(), key)
	if err != nil {
		if err == target.ErrNotFound {
			return false
		}
		t.Fatalf("head object %q: %v", key, err)
	}
	return true
}

// ---------------------------------------------------------------------------
// Deploy tests
// ---------------------------------------------------------------------------

func TestDeploy(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"README.md": "# Hello\n",
		"main.py":   "print('hello')\n",
	})

	input := defaultDeployInput(b)
	result := deployToTarget(t, eng, tgt, input)

	// Verify result fields are populated.
	if result.DeploymentID == "" {
		t.Error("DeploymentID is empty")
	}
	if result.BundleHash == "" {
		t.Error("BundleHash is empty")
	}
	if len(result.ManifestJSON) == 0 {
		t.Error("ManifestJSON is empty")
	}
	if result.TargetName != "test" {
		t.Errorf("TargetName = %q, want %q", result.TargetName, "test")
	}

	// Verify ACTIVE pointer exists and contains the deployment ID.
	activeKey := "my-skill/.agentctx/ACTIVE"
	activeData := readObject(t, tgt, activeKey)
	if got := strings.TrimSpace(string(activeData)); got != result.DeploymentID {
		t.Errorf("ACTIVE = %q, want %q", got, result.DeploymentID)
	}

	// Verify manifest.json exists at expected path.
	manifestKey := "my-skill/.agentctx/deployments/" + result.DeploymentID + "/manifest.json"
	if !objectExists(t, tgt, manifestKey) {
		t.Errorf("manifest.json not found at %q", manifestKey)
	}

	// Verify manifest content is valid and matches expectations.
	manifestData := readObject(t, tgt, manifestKey)
	m, err := manifest.Unmarshal(manifestData)
	if err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.DeploymentID != result.DeploymentID {
		t.Errorf("manifest.DeploymentID = %q, want %q", m.DeploymentID, result.DeploymentID)
	}
	if m.BundleHash != result.BundleHash {
		t.Errorf("manifest.BundleHash = %q, want %q", m.BundleHash, result.BundleHash)
	}
	if m.CanonicalStore != "s3://test-bucket" {
		t.Errorf("manifest.CanonicalStore = %q, want %q", m.CanonicalStore, "s3://test-bucket")
	}
	if m.ProviderVersion != "0.1.0-test" {
		t.Errorf("manifest.ProviderVersion = %q, want %q", m.ProviderVersion, "0.1.0-test")
	}
	if m.ResourceName != "test_resource" {
		t.Errorf("manifest.ResourceName = %q, want %q", m.ResourceName, "test_resource")
	}
	if m.SchemaVersion != 2 {
		t.Errorf("manifest.SchemaVersion = %d, want 2", m.SchemaVersion)
	}
	if m.Origin == nil || m.Origin.Type != "source" {
		t.Errorf("manifest.Origin.Type = %v, want %q", m.Origin, "source")
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest.Files count = %d, want 2", len(m.Files))
	}

	// Verify uploaded files exist at expected paths.
	for _, relPath := range []string{"README.md", "main.py"} {
		fileKey := "my-skill/.agentctx/deployments/" + result.DeploymentID + "/files/" + relPath
		if !objectExists(t, tgt, fileKey) {
			t.Errorf("file not found at %q", fileKey)
		}
	}

	// Verify file content matches.
	readmeKey := "my-skill/.agentctx/deployments/" + result.DeploymentID + "/files/README.md"
	readmeData := readObject(t, tgt, readmeKey)
	if string(readmeData) != "# Hello\n" {
		t.Errorf("README.md content = %q, want %q", string(readmeData), "# Hello\n")
	}
}

// TestDeploy_ManifestResourceTypeIsSkill verifies that the deployed manifest
// has ResourceType = "skill" (fix #4: was previously "agentctx_skill_version").
func TestDeploy_ManifestResourceTypeIsSkill(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"main.py": "print('hello')\n",
	})

	input := defaultDeployInput(b)
	result := deployToTarget(t, eng, tgt, input)

	// Read and parse the manifest.
	manifestKey := "my-skill/.agentctx/deployments/" + result.DeploymentID + "/manifest.json"
	manifestData := readObject(t, tgt, manifestKey)
	m, err := manifest.Unmarshal(manifestData)
	if err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	// Fix #4: ResourceType must be "skill", not "agentctx_skill_version".
	if m.ResourceType != "skill" {
		t.Errorf("manifest.ResourceType = %q, want %q", m.ResourceType, "skill")
	}
}

// TestDeploy_WithRegistryInfo verifies that when RegistryInfo is provided,
// the manifest origin type is "registry" and the registry info is included.
func TestDeploy_WithRegistryInfo(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"main.py": "print('hello')\n",
	})

	input := defaultDeployInput(b)
	input.RegistryInfo = &manifest.ManifestRegistry{
		Type:       "anthropic",
		SkillID:    "skill-abc-123",
		Version:    "1771039616808221",
		BundleHash: b.BundleHash,
	}

	result := deployToTarget(t, eng, tgt, input)

	manifestKey := "my-skill/.agentctx/deployments/" + result.DeploymentID + "/manifest.json"
	manifestData := readObject(t, tgt, manifestKey)
	m, err := manifest.Unmarshal(manifestData)
	if err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if m.Origin == nil {
		t.Fatal("manifest.Origin is nil")
	}
	if m.Origin.Type != "registry" {
		t.Errorf("manifest.Origin.Type = %q, want %q", m.Origin.Type, "registry")
	}
	if m.Registry == nil {
		t.Fatal("manifest.Registry is nil")
	}
	if m.Registry.SkillID != "skill-abc-123" {
		t.Errorf("manifest.Registry.SkillID = %q, want %q", m.Registry.SkillID, "skill-abc-123")
	}
	if m.Registry.Version != "1771039616808221" {
		t.Errorf("manifest.Registry.Version = %q, want %q", m.Registry.Version, "1771039616808221")
	}
	// ResourceType should still be "skill".
	if m.ResourceType != "skill" {
		t.Errorf("manifest.ResourceType = %q, want %q", m.ResourceType, "skill")
	}
}

func TestDeploy_ConditionalUpdate(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b1 := createTempBundle(t, map[string]string{
		"file.txt": "version 1",
	})

	// First deploy -- no PreviousDeployID.
	input1 := defaultDeployInput(b1)
	result1 := deployToTarget(t, eng, tgt, input1)

	// Second deploy with PreviousDeployID set.
	b2 := createTempBundle(t, map[string]string{
		"file.txt": "version 2",
	})
	input2 := defaultDeployInput(b2)
	input2.PreviousDeployID = result1.DeploymentID

	result2, err := eng.Deploy(context.Background(), tgt, input2)
	if err != nil {
		t.Fatalf("conditional deploy failed: %v", err)
	}

	if result2.DeploymentID == result1.DeploymentID {
		t.Error("second deploy should have a different deployment ID")
	}

	// ACTIVE should now point to the new deployment.
	activeData := readObject(t, tgt, "my-skill/.agentctx/ACTIVE")
	if got := strings.TrimSpace(string(activeData)); got != result2.DeploymentID {
		t.Errorf("ACTIVE = %q, want %q", got, result2.DeploymentID)
	}

	// Verify the new file content.
	fileKey := "my-skill/.agentctx/deployments/" + result2.DeploymentID + "/files/file.txt"
	fileData := readObject(t, tgt, fileKey)
	if string(fileData) != "version 2" {
		t.Errorf("file.txt content = %q, want %q", string(fileData), "version 2")
	}
}

func TestDeploy_WithStagedCleanup(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	// Simulate a previously staged deployment by writing objects under its prefix.
	ctx := context.Background()
	stagedID := "dep_20260101T000000Z_deadbeef"
	stagedPrefix := "my-skill/.agentctx/deployments/" + stagedID + "/"
	_ = tgt.Put(ctx, stagedPrefix+"files/leftover.txt", bytes.NewReader([]byte("staged")), target.PutOptions{})
	_ = tgt.Put(ctx, stagedPrefix+"manifest.json", bytes.NewReader([]byte("{}")), target.PutOptions{})

	// Deploy with StagedDeployID should clean up the staged objects.
	b := createTempBundle(t, map[string]string{
		"app.py": "print('app')\n",
	})
	input := defaultDeployInput(b)
	input.StagedDeployID = stagedID

	result := deployToTarget(t, eng, tgt, input)

	// Staged objects should be gone.
	if objectExists(t, tgt, stagedPrefix+"files/leftover.txt") {
		t.Error("staged file leftover.txt should have been cleaned up")
	}
	if objectExists(t, tgt, stagedPrefix+"manifest.json") {
		t.Error("staged manifest.json should have been cleaned up")
	}

	// New deployment should exist.
	if result.DeploymentID == "" {
		t.Error("new deployment ID is empty")
	}
}

// ---------------------------------------------------------------------------
// Refresh tests
// ---------------------------------------------------------------------------

func TestRefresh(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"index.md": "# Index\n",
	})
	input := defaultDeployInput(b)
	deployResult := deployToTarget(t, eng, tgt, input)

	// Refresh with the correct expected hash.
	result, err := eng.Refresh(context.Background(), tgt, "my-skill", deployResult.BundleHash, false)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if !result.Healthy {
		t.Error("expected Healthy = true")
	}
	if result.Drifted {
		t.Error("expected Drifted = false")
	}
	if result.MissingManifest {
		t.Error("expected MissingManifest = false")
	}
	if result.ActiveDeploymentID != deployResult.DeploymentID {
		t.Errorf("ActiveDeploymentID = %q, want %q", result.ActiveDeploymentID, deployResult.DeploymentID)
	}
	if result.Manifest == nil {
		t.Fatal("expected Manifest to be non-nil")
	}
	if result.Manifest.BundleHash != deployResult.BundleHash {
		t.Errorf("Manifest.BundleHash = %q, want %q", result.Manifest.BundleHash, deployResult.BundleHash)
	}
	if result.TargetName != "test" {
		t.Errorf("TargetName = %q, want %q", result.TargetName, "test")
	}
}

func TestRefresh_DeepCheck(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"file1.txt": "content1",
		"file2.txt": "content2",
		"sub/f3.md": "# Sub\n",
	})
	input := defaultDeployInput(b)
	deployResult := deployToTarget(t, eng, tgt, input)

	// Deep check should verify all files exist.
	result, err := eng.Refresh(context.Background(), tgt, "my-skill", deployResult.BundleHash, true)
	if err != nil {
		t.Fatalf("refresh deep check failed: %v", err)
	}

	if !result.Healthy {
		t.Errorf("expected Healthy = true, MissingFiles = %v", result.MissingFiles)
	}
	if len(result.MissingFiles) != 0 {
		t.Errorf("expected no missing files, got %v", result.MissingFiles)
	}
}

func TestRefresh_MissingManifest(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	// Refresh on a target with no deployments.
	result, err := eng.Refresh(context.Background(), tgt, "my-skill", "", false)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	// No ACTIVE pointer exists, so ActiveDeploymentID should be empty.
	if result.ActiveDeploymentID != "" {
		t.Errorf("ActiveDeploymentID = %q, want empty", result.ActiveDeploymentID)
	}
	// When no ACTIVE exists, Healthy defaults to false (no manifest to check).
	if result.Healthy {
		t.Error("expected Healthy = false when no deployment exists")
	}
}

func TestRefresh_MissingManifestWithActivePointer(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")
	ctx := context.Background()

	// Write an ACTIVE pointer that points to a deployment with no manifest.
	fakeDepID := "dep_20260101T000000Z_aabbccdd"
	activeKey := "my-skill/.agentctx/ACTIVE"
	_ = tgt.Put(ctx, activeKey, bytes.NewReader([]byte(fakeDepID)), target.PutOptions{})

	result, err := eng.Refresh(ctx, tgt, "my-skill", "", false)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if result.ActiveDeploymentID != fakeDepID {
		t.Errorf("ActiveDeploymentID = %q, want %q", result.ActiveDeploymentID, fakeDepID)
	}
	if !result.MissingManifest {
		t.Error("expected MissingManifest = true")
	}
	if result.Healthy {
		t.Error("expected Healthy = false when manifest is missing")
	}
}

func TestRefresh_DriftDetected(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"data.json": `{"key": "value"}`,
	})
	input := defaultDeployInput(b)
	deployResult := deployToTarget(t, eng, tgt, input)

	// Refresh with a wrong expected hash to trigger drift detection.
	wrongHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	result, err := eng.Refresh(context.Background(), tgt, "my-skill", wrongHash, false)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if !result.Drifted {
		t.Error("expected Drifted = true when expectedBundleHash does not match")
	}
	if result.ActiveDeploymentID != deployResult.DeploymentID {
		t.Errorf("ActiveDeploymentID = %q, want %q", result.ActiveDeploymentID, deployResult.DeploymentID)
	}
	// Even with drift, healthy should be true since all objects are present.
	if !result.Healthy {
		t.Error("expected Healthy = true even when drifted (objects still present)")
	}
}

func TestRefresh_NoExpectedHash(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"file.txt": "content",
	})
	input := defaultDeployInput(b)
	deployToTarget(t, eng, tgt, input)

	// Refresh with empty expected hash -- should not report drift.
	result, err := eng.Refresh(context.Background(), tgt, "my-skill", "", false)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if result.Drifted {
		t.Error("expected Drifted = false when expectedBundleHash is empty")
	}
	if !result.Healthy {
		t.Error("expected Healthy = true")
	}
}

// ---------------------------------------------------------------------------
// Prune tests
// ---------------------------------------------------------------------------

func TestPrune(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	// Deploy 5 times, collecting all deployment IDs.
	deployIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		b := createTempBundle(t, map[string]string{
			"file.txt": strings.Repeat("x", i+1), // vary content so hash differs
		})
		input := defaultDeployInput(b)
		if i > 0 {
			input.PreviousDeployID = deployIDs[i-1]
		}
		result := deployToTarget(t, eng, tgt, input)
		deployIDs[i] = result.DeploymentID
		// Small sleep to ensure timestamps differ in deployment IDs.
		time.Sleep(10 * time.Millisecond)
	}

	// The ACTIVE pointer should point to the last deployment.
	activeData := readObject(t, tgt, "my-skill/.agentctx/ACTIVE")
	activeID := strings.TrimSpace(string(activeData))
	if activeID != deployIDs[4] {
		t.Fatalf("ACTIVE = %q, want %q", activeID, deployIDs[4])
	}

	// Prune with retain=2. The active deployment (deployIDs[4]) is excluded
	// from candidates, so the candidates are deployIDs[0..3]. Of those 4,
	// retain 2 means prune the 2 oldest: deployIDs[0] and deployIDs[1].
	pruned, err := eng.Prune(context.Background(), tgt, "my-skill", activeID, deployIDs, 2)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(pruned) != 2 {
		t.Errorf("pruned count = %d, want 2", len(pruned))
	}

	// Verify the pruned deployments are the oldest non-active ones.
	prunedSet := make(map[string]bool, len(pruned))
	for _, id := range pruned {
		prunedSet[id] = true
	}
	if !prunedSet[deployIDs[0]] {
		t.Errorf("expected deployIDs[0] (%s) to be pruned", deployIDs[0])
	}
	if !prunedSet[deployIDs[1]] {
		t.Errorf("expected deployIDs[1] (%s) to be pruned", deployIDs[1])
	}

	// Verify pruned deployment objects are actually gone.
	for _, prunedID := range pruned {
		prefix := "my-skill/.agentctx/deployments/" + prunedID + "/"
		objects, err := tgt.List(context.Background(), prefix)
		if err != nil {
			t.Fatalf("listing prefix %q: %v", prefix, err)
		}
		if len(objects) != 0 {
			t.Errorf("pruned deployment %s still has %d objects", prunedID, len(objects))
		}
	}

	// Verify non-pruned deployments still exist.
	for _, keepID := range []string{deployIDs[2], deployIDs[3], deployIDs[4]} {
		manifestKey := "my-skill/.agentctx/deployments/" + keepID + "/manifest.json"
		if !objectExists(t, tgt, manifestKey) {
			t.Errorf("kept deployment %s has missing manifest", keepID)
		}
	}

	// Verify ACTIVE pointer is unchanged.
	activeAfter := readObject(t, tgt, "my-skill/.agentctx/ACTIVE")
	if strings.TrimSpace(string(activeAfter)) != activeID {
		t.Error("ACTIVE pointer changed during prune")
	}
}

func TestPrune_RetainAll(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	// Deploy 3 times.
	deployIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		b := createTempBundle(t, map[string]string{
			"file.txt": strings.Repeat("y", i+1),
		})
		input := defaultDeployInput(b)
		if i > 0 {
			input.PreviousDeployID = deployIDs[i-1]
		}
		result := deployToTarget(t, eng, tgt, input)
		deployIDs[i] = result.DeploymentID
		time.Sleep(10 * time.Millisecond)
	}

	activeID := deployIDs[2]

	// Prune with retain >= number of non-active candidates (2).
	// candidates = deployIDs[0], deployIDs[1] (2 items), retain = 5.
	pruned, err := eng.Prune(context.Background(), tgt, "my-skill", activeID, deployIDs, 5)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(pruned) != 0 {
		t.Errorf("pruned count = %d, want 0 (retain >= candidates)", len(pruned))
	}

	// All deployments should still exist.
	for _, id := range deployIDs {
		manifestKey := "my-skill/.agentctx/deployments/" + id + "/manifest.json"
		if !objectExists(t, tgt, manifestKey) {
			t.Errorf("deployment %s manifest missing after retain-all prune", id)
		}
	}
}

func TestPrune_NoCandidates(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	// Deploy once.
	b := createTempBundle(t, map[string]string{"f.txt": "data"})
	input := defaultDeployInput(b)
	result := deployToTarget(t, eng, tgt, input)

	// Prune with the only deployment being active -- no candidates.
	pruned, err := eng.Prune(context.Background(), tgt, "my-skill", result.DeploymentID, []string{result.DeploymentID}, 0)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(pruned) != 0 {
		t.Errorf("pruned count = %d, want 0 (no candidates besides active)", len(pruned))
	}
}

// ---------------------------------------------------------------------------
// Destroy tests
// ---------------------------------------------------------------------------

func TestDestroy_Force(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	b := createTempBundle(t, map[string]string{
		"main.py":   "print('hello')\n",
		"config.yml": "key: value\n",
	})
	input := defaultDeployInput(b)
	deployResult := deployToTarget(t, eng, tgt, input)

	// Verify objects exist before destroy.
	activeKey := "my-skill/.agentctx/ACTIVE"
	if !objectExists(t, tgt, activeKey) {
		t.Fatal("ACTIVE should exist before destroy")
	}

	// Force destroy (scoped to .agentctx/).
	err := eng.Destroy(context.Background(), tgt, "my-skill", engine.DestroyOptions{
		ForceDestroy:     true,
		ActiveDeployID:   deployResult.DeploymentID,
		ManagedDeployIDs: []string{deployResult.DeploymentID},
	})
	if err != nil {
		t.Fatalf("force destroy failed: %v", err)
	}

	// All objects under .agentctx/ should be gone.
	objects, err := tgt.List(context.Background(), "my-skill/.agentctx/")
	if err != nil {
		t.Fatalf("listing after destroy: %v", err)
	}
	if len(objects) != 0 {
		keys := make([]string, len(objects))
		for i, o := range objects {
			keys[i] = o.Key
		}
		t.Errorf("expected 0 objects after force destroy, got %d: %v", len(objects), keys)
	}
}

func TestDestroy_ForceWithSharedPrefix(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")
	ctx := context.Background()

	// Deploy a skill.
	b := createTempBundle(t, map[string]string{
		"main.py": "code",
	})
	input := defaultDeployInput(b)
	deployResult := deployToTarget(t, eng, tgt, input)

	// Also put some non-managed content under the skill prefix.
	_ = tgt.Put(ctx, "my-skill/custom-file.txt", bytes.NewReader([]byte("custom")), target.PutOptions{})

	// Force destroy with shared prefix = true should delete everything.
	err := eng.Destroy(ctx, tgt, "my-skill", engine.DestroyOptions{
		ForceDestroy:             true,
		ForceDestroySharedPrefix: true,
		ActiveDeployID:           deployResult.DeploymentID,
		ManagedDeployIDs:         []string{deployResult.DeploymentID},
	})
	if err != nil {
		t.Fatalf("force destroy with shared prefix failed: %v", err)
	}

	// Everything under my-skill/ should be gone.
	objects, err := tgt.List(ctx, "my-skill/")
	if err != nil {
		t.Fatalf("listing after destroy: %v", err)
	}
	if len(objects) != 0 {
		t.Errorf("expected 0 objects after force destroy with shared prefix, got %d", len(objects))
	}
}

func TestDestroy_Graceful(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")
	ctx := context.Background()

	// Deploy twice to have two managed deployments.
	b1 := createTempBundle(t, map[string]string{"file.txt": "v1"})
	input1 := defaultDeployInput(b1)
	result1 := deployToTarget(t, eng, tgt, input1)

	b2 := createTempBundle(t, map[string]string{"file.txt": "v2"})
	input2 := defaultDeployInput(b2)
	input2.PreviousDeployID = result1.DeploymentID
	result2 := deployToTarget(t, eng, tgt, input2)

	managedIDs := []string{result1.DeploymentID, result2.DeploymentID}

	// Graceful destroy.
	err := eng.Destroy(ctx, tgt, "my-skill", engine.DestroyOptions{
		ForceDestroy:     false,
		ManagedDeployIDs: managedIDs,
		ActiveDeployID:   result2.DeploymentID,
	})
	if err != nil {
		t.Fatalf("graceful destroy failed: %v", err)
	}

	// Both managed deployments should be deleted.
	for _, id := range managedIDs {
		prefix := "my-skill/.agentctx/deployments/" + id + "/"
		objects, err := tgt.List(ctx, prefix)
		if err != nil {
			t.Fatalf("listing deployment %s: %v", id, err)
		}
		if len(objects) != 0 {
			t.Errorf("deployment %s still has %d objects after graceful destroy", id, len(objects))
		}
	}

	// ACTIVE pointer should also be deleted since it pointed to a managed deployment.
	if objectExists(t, tgt, "my-skill/.agentctx/ACTIVE") {
		t.Error("ACTIVE pointer should be deleted in graceful destroy when it points to a managed deployment")
	}
}

func TestDestroy_GracefulPreservesUnmanaged(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")
	ctx := context.Background()

	// Deploy one managed deployment.
	b := createTempBundle(t, map[string]string{"file.txt": "managed"})
	input := defaultDeployInput(b)
	result := deployToTarget(t, eng, tgt, input)

	// Simulate an unmanaged deployment by manually writing objects.
	unmanagedID := "dep_20260101T000000Z_aabbccdd"
	unmanagedPrefix := "my-skill/.agentctx/deployments/" + unmanagedID + "/"
	_ = tgt.Put(ctx, unmanagedPrefix+"manifest.json", bytes.NewReader([]byte("{}")), target.PutOptions{})
	_ = tgt.Put(ctx, unmanagedPrefix+"files/other.txt", bytes.NewReader([]byte("other")), target.PutOptions{})

	// Overwrite ACTIVE to point to the unmanaged deployment.
	_ = tgt.Put(ctx, "my-skill/.agentctx/ACTIVE", bytes.NewReader([]byte(unmanagedID)), target.PutOptions{})

	// Graceful destroy should only delete managed deployments.
	err := eng.Destroy(ctx, tgt, "my-skill", engine.DestroyOptions{
		ForceDestroy:     false,
		ManagedDeployIDs: []string{result.DeploymentID},
		ActiveDeployID:   result.DeploymentID,
	})
	if err != nil {
		t.Fatalf("graceful destroy failed: %v", err)
	}

	// Managed deployment objects should be gone.
	managedPrefix := "my-skill/.agentctx/deployments/" + result.DeploymentID + "/"
	objects, err := tgt.List(ctx, managedPrefix)
	if err != nil {
		t.Fatalf("listing managed deployment: %v", err)
	}
	if len(objects) != 0 {
		t.Errorf("managed deployment should be deleted, still has %d objects", len(objects))
	}

	// Unmanaged deployment should still exist.
	unmanagedObjects, err := tgt.List(ctx, unmanagedPrefix)
	if err != nil {
		t.Fatalf("listing unmanaged deployment: %v", err)
	}
	if len(unmanagedObjects) != 2 {
		t.Errorf("unmanaged deployment should have 2 objects, got %d", len(unmanagedObjects))
	}

	// ACTIVE should NOT be deleted since it points to an unmanaged deployment.
	if !objectExists(t, tgt, "my-skill/.agentctx/ACTIVE") {
		t.Error("ACTIVE should be preserved when it points to an unmanaged deployment")
	}
}

// ---------------------------------------------------------------------------
// CleanupStaged tests
// ---------------------------------------------------------------------------

func TestCleanupStaged(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")
	ctx := context.Background()

	// Manually create objects under a staged deployment prefix.
	stagedID := "dep_20260201T120000Z_11223344"
	prefix := "my-skill/.agentctx/deployments/" + stagedID + "/"

	stagedKeys := []string{
		prefix + "files/a.txt",
		prefix + "files/b.txt",
		prefix + "files/sub/c.md",
		prefix + "manifest.json",
	}

	for _, key := range stagedKeys {
		if err := tgt.Put(ctx, key, bytes.NewReader([]byte("staged-content")), target.PutOptions{}); err != nil {
			t.Fatalf("putting staged object %q: %v", key, err)
		}
	}

	// Verify objects exist before cleanup.
	objects, err := tgt.List(ctx, prefix)
	if err != nil {
		t.Fatalf("listing staged prefix: %v", err)
	}
	if len(objects) != len(stagedKeys) {
		t.Fatalf("expected %d staged objects, got %d", len(stagedKeys), len(objects))
	}

	// Clean up the staged deployment.
	if err := eng.CleanupStaged(ctx, tgt, "my-skill", stagedID); err != nil {
		t.Fatalf("cleanup staged failed: %v", err)
	}

	// All staged objects should be gone.
	objects, err = tgt.List(ctx, prefix)
	if err != nil {
		t.Fatalf("listing after cleanup: %v", err)
	}
	if len(objects) != 0 {
		keys := make([]string, len(objects))
		for i, o := range objects {
			keys[i] = o.Key
		}
		t.Errorf("expected 0 objects after cleanup, got %d: %v", len(objects), keys)
	}
}

func TestCleanupStaged_NoObjects(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")

	// Cleaning up a non-existent staged deployment should not error.
	err := eng.CleanupStaged(context.Background(), tgt, "my-skill", "dep_20260101T000000Z_deadbeef")
	if err != nil {
		t.Fatalf("cleanup staged with no objects should not error: %v", err)
	}
}

func TestCleanupStaged_PreservesOtherDeployments(t *testing.T) {
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("test")
	ctx := context.Background()

	// Create two deployments: one to clean up, one to keep.
	stagedID := "dep_20260201T120000Z_11223344"
	keepID := "dep_20260202T120000Z_55667788"

	stagedPrefix := "my-skill/.agentctx/deployments/" + stagedID + "/"
	keepPrefix := "my-skill/.agentctx/deployments/" + keepID + "/"

	_ = tgt.Put(ctx, stagedPrefix+"files/a.txt", bytes.NewReader([]byte("staged")), target.PutOptions{})
	_ = tgt.Put(ctx, keepPrefix+"files/b.txt", bytes.NewReader([]byte("keep")), target.PutOptions{})
	_ = tgt.Put(ctx, keepPrefix+"manifest.json", bytes.NewReader([]byte("{}")), target.PutOptions{})

	// Clean up only the staged deployment.
	if err := eng.CleanupStaged(ctx, tgt, "my-skill", stagedID); err != nil {
		t.Fatalf("cleanup staged failed: %v", err)
	}

	// Staged objects should be gone.
	if objectExists(t, tgt, stagedPrefix+"files/a.txt") {
		t.Error("staged file should have been cleaned up")
	}

	// Kept deployment should be untouched.
	if !objectExists(t, tgt, keepPrefix+"files/b.txt") {
		t.Error("kept deployment file should still exist")
	}
	if !objectExists(t, tgt, keepPrefix+"manifest.json") {
		t.Error("kept deployment manifest should still exist")
	}
}

// ---------------------------------------------------------------------------
// Integration scenario tests
// ---------------------------------------------------------------------------

func TestDeployRefreshPruneDestroy(t *testing.T) {
	// End-to-end scenario: deploy, refresh, prune, destroy.
	eng := newTestEngine()
	tgt := target.NewMemoryTarget("integration")
	ctx := context.Background()

	// Step 1: Deploy.
	b := createTempBundle(t, map[string]string{
		"tool.py":   "def run(): pass\n",
		"config.yml": "enabled: true\n",
	})
	input := defaultDeployInput(b)
	result := deployToTarget(t, eng, tgt, input)

	// Step 2: Refresh and verify healthy.
	refreshResult, err := eng.Refresh(ctx, tgt, "my-skill", result.BundleHash, true)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if !refreshResult.Healthy {
		t.Errorf("expected healthy after deploy, MissingFiles=%v", refreshResult.MissingFiles)
	}
	if refreshResult.Drifted {
		t.Error("expected no drift after deploy")
	}

	// Step 3: Deploy a second version.
	b2 := createTempBundle(t, map[string]string{
		"tool.py":   "def run(): return True\n",
		"config.yml": "enabled: false\n",
	})
	input2 := defaultDeployInput(b2)
	input2.PreviousDeployID = result.DeploymentID
	result2 := deployToTarget(t, eng, tgt, input2)

	// Step 4: Prune old deployment (retain=0 of non-active).
	pruned, err := eng.Prune(ctx, tgt, "my-skill", result2.DeploymentID, []string{result.DeploymentID, result2.DeploymentID}, 0)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != result.DeploymentID {
		t.Errorf("expected to prune first deployment, pruned = %v", pruned)
	}

	// Step 5: Force destroy.
	err = eng.Destroy(ctx, tgt, "my-skill", engine.DestroyOptions{
		ForceDestroy:     true,
		ManagedDeployIDs: []string{result2.DeploymentID},
		ActiveDeployID:   result2.DeploymentID,
	})
	if err != nil {
		t.Fatalf("destroy failed: %v", err)
	}

	// Verify everything is gone.
	objects, err := tgt.List(ctx, "my-skill/")
	if err != nil {
		t.Fatalf("final listing: %v", err)
	}
	if len(objects) != 0 {
		t.Errorf("expected 0 objects after destroy, got %d", len(objects))
	}
}
