package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestIntegration_SkillLifecycle exercises the full Anthropic Skills API:
//
//	create skill → create version (multipart upload) → list versions →
//	delete version → delete skill
//
// Skipped unless ANTHROPIC_API_KEY is set. Run with:
//
//	ANTHROPIC_API_KEY=sk-ant-... go test ./internal/anthropic/ -run TestIntegration -v -timeout 120s
func TestIntegration_SkillLifecycle(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := NewClient(ClientConfig{
		APIKey:         apiKey,
		MaxRetries:     3,
		TimeoutSeconds: 30,
		DestroyRemote:  true,
	})

	// ---------------------------------------------------------------
	// 1. Build test source directory
	// ---------------------------------------------------------------
	// The directory name must match the skill name in SKILL.md.
	skillName := uniqueSkillName("integration-test-skill")
	tmpRoot := t.TempDir()
	sourceDir := tmpRoot + "/" + skillName
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir sourceDir: %v", err)
	}

	testFiles := map[string]string{
		"SKILL.md":     "---\nname: " + skillName + "\ndescription: A test skill for integration testing\n---\n\n# Integration Test Skill\n\nThis is a test skill.\n",
		"main.py":      "print('hello from integration test')\n",
		"config.yaml":  "model: claude-3\ntemperature: 0.7\n",
		"lib/utils.py": "def greet(name):\n    return f'Hello, {name}!'\n",
	}
	for name, content := range testFiles {
		path := sourceDir + "/" + name
		if idx := lastSlash(name); idx >= 0 {
			if err := os.MkdirAll(sourceDir+"/"+name[:idx], 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", name[:idx], err)
			}
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// ---------------------------------------------------------------
	// 2. Create skill (multipart file upload)
	// ---------------------------------------------------------------
	displayTitle := "Integration Test Skill " + time.Now().Format("20060102T150405")
	t.Logf("Creating skill with title %q...", displayTitle)
	skill := createSkillWithRetry(t, ctx, client, sourceDir, displayTitle)
	t.Logf("Created skill: id=%s title=%q type=%s source=%s", skill.ID, skill.DisplayTitle, skill.Type, skill.Source)

	// Always clean up the skill, even if later steps fail.
	// The API requires deleting all versions before deleting the skill.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()

		// NOTE: The Anthropic API may return 500 when deleting the "latest"
		// version of a skill. This appears to be an API-side limitation.
		// Cleanup is best-effort.
		t.Log("Cleaning up: deleting all versions (newest first)...")
		versions, err := client.ListVersions(cleanupCtx, skill.ID)
		if err != nil {
			t.Logf("WARNING: failed to list versions for cleanup: %v", err)
		} else {
			// Delete in reverse order (newest first) to avoid deleting the
			// "latest" version while older ones still exist.
			for i := len(versions) - 1; i >= 0; i-- {
				v := versions[i]
				if err := client.DeleteVersion(cleanupCtx, skill.ID, v.Version); err != nil {
					t.Logf("WARNING: failed to delete version %s: %v", v.Version, err)
				} else {
					t.Logf("Deleted version %s", v.Version)
				}
			}
		}

		t.Log("Cleaning up: deleting skill...")
		if err := client.DeleteSkill(cleanupCtx, skill.ID); err != nil {
			t.Logf("WARNING: failed to delete skill %s: %v", skill.ID, err)
		} else {
			t.Logf("Deleted skill %s", skill.ID)
		}
	})

	if skill.ID == "" {
		t.Fatal("skill.ID is empty")
	}
	if skill.Type != "skill" {
		t.Errorf("skill.Type = %q, want %q", skill.Type, "skill")
	}

	// ---------------------------------------------------------------
	// 3. Get skill (verify it exists)
	// ---------------------------------------------------------------
	t.Log("Getting skill...")
	got, err := client.GetSkill(ctx, skill.ID)
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.ID != skill.ID {
		t.Errorf("GetSkill ID = %q, want %q", got.ID, skill.ID)
	}
	t.Logf("GetSkill: display_title=%q latest_version=%s", got.DisplayTitle, got.LatestVersion)

	// ---------------------------------------------------------------
	// 4. List versions (should have the initial version from CreateSkill)
	// ---------------------------------------------------------------
	t.Log("Listing versions...")
	versions, err := client.ListVersions(ctx, skill.ID)
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	t.Logf("Listed %d version(s)", len(versions))
	for i, v := range versions {
		t.Logf("  version[%d]: id=%s version=%s name=%s directory=%s", i, v.ID, v.Version, v.Name, v.Directory)
	}

	// ---------------------------------------------------------------
	// 5. Create additional version
	// ---------------------------------------------------------------
	if len(versions) > 0 {
		t.Log("Creating additional version...")
		// Add a new file to make a distinct version.
		os.WriteFile(sourceDir+"/extra.txt", []byte("additional file\n"), 0o644)
		ver2 := createVersionWithRetry(t, ctx, client, skill.ID, sourceDir)
		t.Logf("Created version: id=%s version=%s", ver2.ID, ver2.Version)

		// List again to verify.
		versions2, err := client.ListVersions(ctx, skill.ID)
		if err != nil {
			t.Fatalf("ListVersions (after v2) failed: %v", err)
		}
		t.Logf("Now have %d version(s)", len(versions2))
	}

	// ---------------------------------------------------------------
	// 6. Cleanup happens via t.Cleanup (delete skill)
	// ---------------------------------------------------------------
	t.Log("Integration test complete, cleanup will run via t.Cleanup")
}

// TestIntegration_GetVersionByVersionString exercises fix #1:
// GetVersion and DeleteVersion must use the version *string* (e.g. "1771039616808221")
// in the API path, not the version resource ID (e.g. "skill_version_01...").
//
// Skipped unless ANTHROPIC_API_KEY is set.
func TestIntegration_GetVersionByVersionString(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := NewClient(ClientConfig{
		APIKey:         apiKey,
		MaxRetries:     3,
		TimeoutSeconds: 30,
		DestroyRemote:  true,
	})

	// Create a skill and version.
	skillName := uniqueSkillName("version-string-test")
	tmpRoot := t.TempDir()
	sourceDir := tmpRoot + "/" + skillName
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir sourceDir: %v", err)
	}
	if err := os.WriteFile(sourceDir+"/SKILL.md", []byte("---\nname: "+skillName+"\ndescription: test\n---\n# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceDir+"/main.py", []byte("print('test')\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	displayTitle := "Version String Test " + time.Now().Format("20060102T150405")
	skill := createSkillWithRetry(t, ctx, client, sourceDir, displayTitle)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		versions, _ := client.ListVersions(cleanupCtx, skill.ID)
		for i := len(versions) - 1; i >= 0; i-- {
			client.DeleteVersion(cleanupCtx, skill.ID, versions[i].Version)
		}
		client.DeleteSkill(cleanupCtx, skill.ID)
	})

	// Create an additional version.
	ver := createVersionWithRetry(t, ctx, client, skill.ID, sourceDir)
	t.Logf("Created version: id=%s version=%s", ver.ID, ver.Version)

	// Sanity check: ID and Version should differ.
	if ver.ID == ver.Version {
		t.Logf("Warning: version ID and version string are the same (%s)", ver.ID)
	}

	// Fix #1: GetVersion using the version *string*, not the ID.
	got, err := client.GetVersion(ctx, skill.ID, ver.Version)
	if err != nil {
		t.Fatalf("GetVersion(version=%q) failed: %v", ver.Version, err)
	}
	if got.Version != ver.Version {
		t.Errorf("GetVersion returned Version=%q, want %q", got.Version, ver.Version)
	}
	if got.ID != ver.ID {
		t.Errorf("GetVersion returned ID=%q, want %q", got.ID, ver.ID)
	}
	t.Logf("GetVersion succeeded with version string %q", ver.Version)

	// Fix #1: DeleteVersion using the version *string*, not the ID.
	// List versions and delete the OLDEST one first — the Anthropic API may
	// return 500 when deleting the "latest" version while others still exist.
	versions, listErr := client.ListVersions(ctx, skill.ID)
	if listErr != nil {
		t.Fatalf("ListVersions failed: %v", listErr)
	}
	if len(versions) < 2 {
		t.Skipf("expected >=2 versions for delete test, got %d; skipping delete portion", len(versions))
	}

	// Delete the oldest (first) version — it is not "latest" so the API accepts it.
	oldest := versions[0]
	t.Logf("Deleting oldest version %s (not the latest)...", oldest.Version)
	err = client.DeleteVersion(ctx, skill.ID, oldest.Version)
	if err != nil {
		t.Fatalf("DeleteVersion(version=%q) failed: %v", oldest.Version, err)
	}
	t.Logf("DeleteVersion succeeded with version string %q", oldest.Version)

	// Verify it's gone: GetVersion should now return 404.
	_, err = client.GetVersion(ctx, skill.ID, oldest.Version)
	if err == nil {
		t.Error("expected GetVersion to fail after deletion, but it succeeded")
	}
}

// TestIntegration_DeleteVersionsBeforeSkill exercises fix #2:
// The API rejects DeleteSkill when versions exist. This test validates that
// (a) DeleteSkill fails with an error when versions exist, and
// (b) a non-latest version can be deleted by its version string.
//
// NOTE: The Anthropic API has a known limitation where deleting the "latest"
// version may return 500. This test works around that by only deleting
// non-latest versions and verifying the skill-deletion constraint.
//
// Skipped unless ANTHROPIC_API_KEY is set.
func TestIntegration_DeleteVersionsBeforeSkill(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := NewClient(ClientConfig{
		APIKey:         apiKey,
		MaxRetries:     3,
		TimeoutSeconds: 30,
		DestroyRemote:  true,
	})

	// Create a skill and version.
	skillName := uniqueSkillName("delete-order-test")
	tmpRoot := t.TempDir()
	sourceDir := tmpRoot + "/" + skillName
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir sourceDir: %v", err)
	}
	if err := os.WriteFile(sourceDir+"/SKILL.md", []byte("---\nname: "+skillName+"\ndescription: test\n---\n# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceDir+"/main.py", []byte("print('test')\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	displayTitle := "Delete Order Test " + time.Now().Format("20060102T150405")
	skill := createSkillWithRetry(t, ctx, client, sourceDir, displayTitle)

	// Safety cleanup in case test fails partway through.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		versions, _ := client.ListVersions(cleanupCtx, skill.ID)
		for _, v := range versions {
			client.DeleteVersion(cleanupCtx, skill.ID, v.Version)
		}
		client.DeleteSkill(cleanupCtx, skill.ID)
	})

	// Create a second version so we have 2 total.
	os.WriteFile(sourceDir+"/extra.txt", []byte("extra file\n"), 0o644)
	ver2 := createVersionWithRetry(t, ctx, client, skill.ID, sourceDir)
	t.Logf("Created additional version: %s", ver2.Version)

	// Fix #2: Verify that versions exist.
	versions, err := client.ListVersions(ctx, skill.ID)
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	if len(versions) < 2 {
		t.Fatalf("expected at least 2 versions, got %d", len(versions))
	}
	t.Logf("Skill has %d version(s)", len(versions))

	// Fix #2: Attempting to delete the skill while versions exist should fail.
	t.Log("Attempting to delete skill with versions (should fail)...")
	deleteErr := client.DeleteSkill(ctx, skill.ID)
	if deleteErr == nil {
		t.Fatal("expected DeleteSkill to fail when versions exist, but it succeeded")
	}
	t.Logf("DeleteSkill correctly rejected: %v", deleteErr)

	// Delete the oldest (non-latest) version to validate fix #1 & #2.
	oldest := versions[0]
	t.Logf("Deleting oldest version %s...", oldest.Version)
	if err := client.DeleteVersion(ctx, skill.ID, oldest.Version); err != nil {
		t.Fatalf("DeleteVersion(%s) failed: %v", oldest.Version, err)
	}
	t.Logf("Deleted version %s", oldest.Version)

	// Verify the deleted version is gone.
	remaining, err := client.ListVersions(ctx, skill.ID)
	if err != nil {
		t.Fatalf("ListVersions after partial deletion failed: %v", err)
	}
	if len(remaining) != len(versions)-1 {
		t.Errorf("expected %d remaining versions, got %d", len(versions)-1, len(remaining))
	}

	// Skill should still not be deletable (1 version remains).
	t.Log("Attempting to delete skill with remaining version (should still fail)...")
	deleteErr = client.DeleteSkill(ctx, skill.ID)
	if deleteErr == nil {
		t.Fatal("expected DeleteSkill to fail with remaining version, but it succeeded")
	}
	t.Logf("DeleteSkill correctly rejected again: %v", deleteErr)
	t.Log("Fix #2 validated: API requires all versions deleted before skill deletion")
}

func createSkillWithRetry(t *testing.T, ctx context.Context, client *Client, sourceDir, displayTitle string) *Skill {
	t.Helper()
	const attempts = 6

	var lastErr error
	for i := 1; i <= attempts; i++ {
		skill, err := client.CreateSkill(ctx, sourceDir, displayTitle)
		if err == nil {
			return skill
		}

		lastErr = err
		retryable := isRetryableLiveErr(err)
		if !retryable {
			t.Fatalf("CreateSkill failed with non-retryable error: %v", err)
		}
		if i == attempts {
			t.Skipf("skipping due upstream Anthropic instability after %d retryable CreateSkill attempt(s): %v", i, err)
		}

		backoff := time.Duration(i) * 2 * time.Second
		t.Logf("CreateSkill attempt %d/%d hit retryable remote error: %v; retrying in %s", i, attempts, err, backoff)
		select {
		case <-ctx.Done():
			if isRetryableLiveErr(lastErr) {
				t.Skipf("skipping due upstream Anthropic instability/context deadline while retrying CreateSkill: %v", lastErr)
			}
			t.Fatalf("context canceled while retrying CreateSkill: %v (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(backoff):
		}
	}

	t.Fatalf("CreateSkill failed: %v", lastErr)
	return nil
}

func createVersionWithRetry(t *testing.T, ctx context.Context, client *Client, skillID, sourceDir string) *SkillVersion {
	t.Helper()
	const attempts = 6

	var lastErr error
	for i := 1; i <= attempts; i++ {
		ver, err := client.CreateVersion(ctx, skillID, sourceDir)
		if err == nil {
			return ver
		}

		lastErr = err
		retryable := isRetryableLiveErr(err)
		if !retryable {
			t.Fatalf("CreateVersion failed with non-retryable error: %v", err)
		}
		if i == attempts {
			t.Skipf("skipping due upstream Anthropic instability after %d retryable CreateVersion attempt(s): %v", i, err)
		}

		backoff := time.Duration(i) * 2 * time.Second
		t.Logf("CreateVersion attempt %d/%d hit retryable remote error: %v; retrying in %s", i, attempts, err, backoff)
		select {
		case <-ctx.Done():
			if isRetryableLiveErr(lastErr) {
				t.Skipf("skipping due upstream Anthropic instability/context deadline while retrying CreateVersion: %v", lastErr)
			}
			t.Fatalf("context canceled while retrying CreateVersion: %v (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(backoff):
		}
	}

	t.Fatalf("CreateVersion failed: %v", lastErr)
	return nil
}

func isRetryableLiveErr(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= http.StatusInternalServerError
	}
	return false
}

func uniqueSkillName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// lastSlash returns the index of the last '/' in s, or -1 if not found.
func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
