package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
)

// ---------------------------------------------------------------------------
// Client construction tests
// ---------------------------------------------------------------------------

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test-key", MaxRetries: -1})

	if c.maxRetries != defaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", c.maxRetries, defaultMaxRetries)
	}
	if c.httpClient.Timeout != time.Duration(defaultTimeoutSeconds)*time.Second {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, time.Duration(defaultTimeoutSeconds)*time.Second)
	}
	if c.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "test-key")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
}

func TestNewClient_Custom(t *testing.T) {
	c := NewClient(ClientConfig{
		APIKey:         "custom-key",
		MaxRetries:     5,
		TimeoutSeconds: 60,
		DestroyRemote:  true,
	})

	if c.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want %d", c.maxRetries, 5)
	}
	if c.httpClient.Timeout != 60*time.Second {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, 60*time.Second)
	}
	if c.apiKey != "custom-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "custom-key")
	}
	if !c.DestroyRemote() {
		t.Error("DestroyRemote() = false, want true")
	}
}

// ---------------------------------------------------------------------------
// Helper: create a client pointed at a test server with no retries (unless
// the test specifically needs retry behaviour).
// ---------------------------------------------------------------------------

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	c := NewClient(ClientConfig{
		APIKey:         "test-api-key",
		MaxRetries:     0, // no retries by default -- overridden in retry tests
		TimeoutSeconds: 5,
	})
	c.baseURL = server.URL
	return c
}

// ---------------------------------------------------------------------------
// Skill fixture used across tests
// ---------------------------------------------------------------------------

const skillFixtureTime = "2026-02-13T12:00:00Z"

func skillJSON() []byte {
	s := Skill{
		ID:           "skill-abc-123",
		DisplayTitle: "My Test Skill",
		CreatedAt:    skillFixtureTime,
		UpdatedAt:    skillFixtureTime,
	}
	b, _ := json.Marshal(s)
	return b
}

// ---------------------------------------------------------------------------
// Skills CRUD tests
// ---------------------------------------------------------------------------

func TestCreateSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/skills" {
			t.Errorf("path = %s, want /v1/skills", r.URL.Path)
		}

		// Verify it's a multipart request.
		ct := r.Header.Get("Content-Type")
		if len(ct) < 9 || ct[:9] != "multipart" {
			t.Errorf("Content-Type = %q, want multipart/*", ct)
		}

		// Parse multipart to verify files and display_title.
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm failed: %v", err)
		}
		if got := r.FormValue("display_title"); got != "My Test Skill" {
			t.Errorf("display_title = %q, want %q", got, "My Test Skill")
		}
		files := r.MultipartForm.File["files[]"]
		if len(files) == 0 {
			t.Error("no files[] uploaded")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(skillJSON())
	}))
	defer server.Close()

	// Create a temp dir with a test file.
	tmpDir := t.TempDir()
	if err := os.WriteFile(tmpDir+"/test.py", []byte("print('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := testClient(t, server)
	skill, err := c.CreateSkill(context.Background(), tmpDir, "My Test Skill")
	if err != nil {
		t.Fatalf("CreateSkill() returned error: %v", err)
	}
	if skill.ID != "skill-abc-123" {
		t.Errorf("skill.ID = %q, want %q", skill.ID, "skill-abc-123")
	}
	if skill.DisplayTitle != "My Test Skill" {
		t.Errorf("skill.DisplayTitle = %q, want %q", skill.DisplayTitle, "My Test Skill")
	}
	if skill.CreatedAt != skillFixtureTime {
		t.Errorf("skill.CreatedAt = %v, want %v", skill.CreatedAt, skillFixtureTime)
	}
}

func TestGetSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/skills/skill-abc-123" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(skillJSON())
	}))
	defer server.Close()

	c := testClient(t, server)
	skill, err := c.GetSkill(context.Background(), "skill-abc-123")
	if err != nil {
		t.Fatalf("GetSkill() returned error: %v", err)
	}
	if skill.ID != "skill-abc-123" {
		t.Errorf("skill.ID = %q, want %q", skill.ID, "skill-abc-123")
	}
	if skill.DisplayTitle != "My Test Skill" {
		t.Errorf("skill.DisplayTitle = %q, want %q", skill.DisplayTitle, "My Test Skill")
	}
}

func TestUpdateSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/v1/skills/skill-abc-123" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123", r.URL.Path)
		}

		var body UpdateSkillRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if body.DisplayTitle != "Updated Title" {
			t.Errorf("display_title = %q, want %q", body.DisplayTitle, "Updated Title")
		}

		updated := Skill{
			ID:           "skill-abc-123",
			DisplayTitle: "Updated Title",
			CreatedAt:    skillFixtureTime,
			UpdatedAt:    "2026-02-13T13:00:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(updated)
	}))
	defer server.Close()

	c := testClient(t, server)
	skill, err := c.UpdateSkill(context.Background(), "skill-abc-123", UpdateSkillRequest{
		DisplayTitle: "Updated Title",
	})
	if err != nil {
		t.Fatalf("UpdateSkill() returned error: %v", err)
	}
	if skill.DisplayTitle != "Updated Title" {
		t.Errorf("skill.DisplayTitle = %q, want %q", skill.DisplayTitle, "Updated Title")
	}
	if skill.UpdatedAt == skill.CreatedAt {
		t.Error("expected UpdatedAt to differ from CreatedAt")
	}
}

func TestDeleteSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/v1/skills/skill-abc-123" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.DeleteSkill(context.Background(), "skill-abc-123")
	if err != nil {
		t.Fatalf("DeleteSkill() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Request validation tests
// ---------------------------------------------------------------------------

func TestRequestHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify required headers.
		if got := r.Header.Get("x-api-key"); got != "test-api-key" {
			t.Errorf("x-api-key = %q, want %q", got, "test-api-key")
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Errorf("anthropic-version = %q, want %q", got, anthropicVersion)
		}
		if anthropicBeta != "" {
			if got := r.Header.Get("anthropic-beta"); got != anthropicBeta {
				t.Errorf("anthropic-beta = %q, want %q", got, anthropicBeta)
			}
		} else {
			if got := r.Header.Get("anthropic-beta"); got != "" {
				t.Errorf("anthropic-beta should not be set when empty, got %q", got)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(skillJSON())
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.GetSkill(context.Background(), "skill-abc-123")
	if err != nil {
		t.Fatalf("GetSkill() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Error handling tests
// ---------------------------------------------------------------------------

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"type":    "invalid_request_error",
			"message": "display_title is required",
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	os.WriteFile(tmpDir+"/test.py", []byte("x"), 0o644)

	c := testClient(t, server)
	_, err := c.CreateSkill(context.Background(), tmpDir, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if apiErr.Type != "invalid_request_error" {
		t.Errorf("Type = %q, want %q", apiErr.Type, "invalid_request_error")
	}
	if apiErr.Message != "display_title is required" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "display_title is required")
	}
}

func TestRetry429(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"type":"rate_limit_error","message":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(skillJSON())
	}))
	defer server.Close()

	// Create a client with enough retries to succeed on the 3rd attempt.
	c := NewClient(ClientConfig{
		APIKey:         "test-api-key",
		MaxRetries:     3,
		TimeoutSeconds: 30,
	})
	c.baseURL = server.URL

	// Override the http client timeout so the test doesn't time out,
	// but the retry backoff (1s, 2s) is inherent to the implementation.
	// We accept that cost for correctness.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	skill, err := c.GetSkill(ctx, "skill-abc-123")
	if err != nil {
		t.Fatalf("GetSkill() returned error after retries: %v", err)
	}
	if skill.ID != "skill-abc-123" {
		t.Errorf("skill.ID = %q, want %q", skill.ID, "skill-abc-123")
	}

	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 3 {
		t.Errorf("server received %d requests, want 3", finalCount)
	}
}

// ---------------------------------------------------------------------------
// MaxRetries: 0 means no retries (fix #8)
// ---------------------------------------------------------------------------

func TestNewClient_ZeroRetries(t *testing.T) {
	// MaxRetries: 0 should mean "no retries" (exactly 1 attempt),
	// NOT fall through to the default of 3.
	c := NewClient(ClientConfig{APIKey: "test-key", MaxRetries: 0})
	if c.maxRetries != 0 {
		t.Errorf("maxRetries = %d, want 0", c.maxRetries)
	}
}

func TestNoRetriesOnMaxRetries0(t *testing.T) {
	// Verify that with MaxRetries: 0 the server is called exactly once,
	// even for retryable 429 errors.
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"type":"rate_limit_error","message":"rate limited"}`))
	}))
	defer server.Close()

	c := testClient(t, server) // MaxRetries: 0
	_, err := c.GetSkill(context.Background(), "skill-abc-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 1 {
		t.Errorf("server received %d requests, want exactly 1 (no retries)", finalCount)
	}
}

// ---------------------------------------------------------------------------
// Version CRUD tests (fixes #1 and #7)
// ---------------------------------------------------------------------------

func versionJSON() []byte {
	v := SkillVersion{
		ID:        "skill_version_01abc",
		Version:   "1771039616808221",
		SkillID:   "skill-abc-123",
		Name:      "integration-test-skill",
		Directory: "integration-test-skill",
		Type:      "skill_version",
		CreatedAt: skillFixtureTime,
	}
	b, _ := json.Marshal(v)
	return b
}

func TestCreateVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		// Fix #7: CreateVersion sends to /v1/skills/{id}/versions with no bundleHash param.
		if r.URL.Path != "/v1/skills/skill-abc-123/versions" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123/versions", r.URL.Path)
		}

		// Should be multipart.
		ct := r.Header.Get("Content-Type")
		if len(ct) < 9 || ct[:9] != "multipart" {
			t.Errorf("Content-Type = %q, want multipart/*", ct)
		}

		// Parse multipart to verify files exist (no bundleHash field).
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm failed: %v", err)
		}
		// Verify there is NO bundle_hash field (fix #7: dead parameter removed).
		if got := r.FormValue("bundle_hash"); got != "" {
			t.Errorf("bundle_hash field should not be present, got %q", got)
		}
		files := r.MultipartForm.File["files[]"]
		if len(files) == 0 {
			t.Error("no files[] uploaded")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(versionJSON())
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	if err := os.WriteFile(tmpDir+"/test.py", []byte("print('hello')"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := testClient(t, server)
	// Fix #7: CreateVersion takes (ctx, skillID, sourceDir) — no bundleHash.
	ver, err := c.CreateVersion(context.Background(), "skill-abc-123", tmpDir)
	if err != nil {
		t.Fatalf("CreateVersion() returned error: %v", err)
	}
	if ver.ID != "skill_version_01abc" {
		t.Errorf("ver.ID = %q, want %q", ver.ID, "skill_version_01abc")
	}
	if ver.Version != "1771039616808221" {
		t.Errorf("ver.Version = %q, want %q", ver.Version, "1771039616808221")
	}
	if ver.SkillID != "skill-abc-123" {
		t.Errorf("ver.SkillID = %q, want %q", ver.SkillID, "skill-abc-123")
	}
}

func TestGetVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		// Fix #1: GetVersion uses the version string (e.g. "1771039616808221")
		// in the URL path, NOT the version resource ID (e.g. "skill_version_01abc").
		if r.URL.Path != "/v1/skills/skill-abc-123/versions/1771039616808221" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123/versions/1771039616808221", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(versionJSON())
	}))
	defer server.Close()

	c := testClient(t, server)
	ver, err := c.GetVersion(context.Background(), "skill-abc-123", "1771039616808221")
	if err != nil {
		t.Fatalf("GetVersion() returned error: %v", err)
	}
	if ver.Version != "1771039616808221" {
		t.Errorf("ver.Version = %q, want %q", ver.Version, "1771039616808221")
	}
	if ver.ID != "skill_version_01abc" {
		t.Errorf("ver.ID = %q, want %q", ver.ID, "skill_version_01abc")
	}
}

func TestGetVersion_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"type":"not_found_error","message":"version not found"}`))
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.GetVersion(context.Background(), "skill-abc-123", "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestDeleteVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		// Fix #1: DeleteVersion uses the version string in the URL path,
		// NOT the version resource ID.
		if r.URL.Path != "/v1/skills/skill-abc-123/versions/1771039616808221" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123/versions/1771039616808221", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.DeleteVersion(context.Background(), "skill-abc-123", "1771039616808221")
	if err != nil {
		t.Fatalf("DeleteVersion() returned error: %v", err)
	}
}

func TestListVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/skills/skill-abc-123/versions" {
			t.Errorf("path = %s, want /v1/skills/skill-abc-123/versions", r.URL.Path)
		}

		resp := ListVersionsResponse{
			Data: []SkillVersion{
				{ID: "sv_1", Version: "100", SkillID: "skill-abc-123", CreatedAt: skillFixtureTime},
				{ID: "sv_2", Version: "200", SkillID: "skill-abc-123", CreatedAt: skillFixtureTime},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := testClient(t, server)
	versions, err := c.ListVersions(context.Background(), "skill-abc-123")
	if err != nil {
		t.Fatalf("ListVersions() returned error: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("len(versions) = %d, want 2", len(versions))
	}
	if versions[0].Version != "100" {
		t.Errorf("versions[0].Version = %q, want %q", versions[0].Version, "100")
	}
	if versions[1].Version != "200" {
		t.Errorf("versions[1].Version = %q, want %q", versions[1].Version, "200")
	}
}

// TestDeleteSkillRequiresNoVersions validates the pattern from fix #2:
// The API rejects DeleteSkill when versions exist (409 Conflict).
// The correct approach is to delete all versions first, then the skill.
func TestDeleteSkillRequiresNoVersions(t *testing.T) {
	var deleteAttempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/skills/skill-abc-123":
			n := atomic.AddInt32(&deleteAttempts, 1)
			if n == 1 {
				// First attempt: reject because versions exist.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"type":"conflict_error","message":"Cannot delete skill with existing versions"}`))
				return
			}
			// Second attempt: succeed (versions were deleted).
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClient(t, server)

	// First delete attempt should fail with 409.
	err := c.DeleteSkill(context.Background(), "skill-abc-123")
	if err == nil {
		t.Fatal("expected error on first delete attempt (versions exist), got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 409 {
		t.Errorf("StatusCode = %d, want 409", apiErr.StatusCode)
	}

	// After deleting versions (simulated), second delete succeeds.
	err = c.DeleteSkill(context.Background(), "skill-abc-123")
	if err != nil {
		t.Fatalf("DeleteSkill() returned error on second attempt: %v", err)
	}
}

// TestAPIError_WrappedError verifies that errors.As can unwrap APIErrors
// that are wrapped by fmt.Errorf (fix #3).
func TestAPIError_WrappedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"type":"not_found_error","message":"skill not found"}`))
	}))
	defer server.Close()

	c := testClient(t, server)
	// GetSkill wraps the APIError with fmt.Errorf("get skill %q: %w", ...)
	_, err := c.GetSkill(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// errors.As should find the *APIError even through wrapping.
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As failed to find *APIError through wrapped error: %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if apiErr.Type != "not_found_error" {
		t.Errorf("Type = %q, want %q", apiErr.Type, "not_found_error")
	}
}

// TestParseAPIError_NestedFormat tests the nested {"type":"error","error":{...}} format.
func TestParseAPIError_NestedFormat(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"not_found_error","message":"The requested resource was not found"}}`)
	apiErr := parseAPIError(404, body)

	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if apiErr.Type != "not_found_error" {
		t.Errorf("Type = %q, want %q", apiErr.Type, "not_found_error")
	}
	if apiErr.Message != "The requested resource was not found" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "The requested resource was not found")
	}
}

// ---------------------------------------------------------------------------
// Verify bundle tests
// ---------------------------------------------------------------------------

func TestVerifyBundle_Valid(t *testing.T) {
	files := map[string][]byte{
		"main.py":    []byte("print('hello world')"),
		"config.yaml": []byte("key: value"),
	}

	// Compute the expected hash using the same algorithm.
	fileHashes := make(map[string]string, len(files))
	for path, data := range files {
		fileHashes[path] = bundle.ComputeFileHashBytes(data)
	}
	expectedHash := bundle.ComputeBundleHash(fileHashes)

	err := VerifyBundle(files, expectedHash)
	if err != nil {
		t.Fatalf("VerifyBundle() returned error for valid bundle: %v", err)
	}
}

func TestVerifyBundle_InvalidHash(t *testing.T) {
	files := map[string][]byte{
		"main.py": []byte("print('hello')"),
	}

	wrongHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	err := VerifyBundle(files, wrongHash)
	if err == nil {
		t.Fatal("VerifyBundle() expected error for invalid hash, got nil")
	}

	var integrityErr *BundleIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("expected *BundleIntegrityError, got %T: %v", err, err)
	}
	if integrityErr.ExpectedHash != wrongHash {
		t.Errorf("ExpectedHash = %q, want %q", integrityErr.ExpectedHash, wrongHash)
	}
	if integrityErr.ActualHash == wrongHash {
		t.Error("ActualHash should differ from the wrong expected hash")
	}
}

func TestVerifyBundle_ModifiedFile(t *testing.T) {
	// Start with a correct bundle and compute its hash.
	originalFiles := map[string][]byte{
		"main.py":    []byte("original content"),
		"config.yaml": []byte("key: value"),
	}

	fileHashes := make(map[string]string, len(originalFiles))
	for path, data := range originalFiles {
		fileHashes[path] = bundle.ComputeFileHashBytes(data)
	}
	expectedHash := bundle.ComputeBundleHash(fileHashes)

	// Now modify one file so that the bundle hash no longer matches.
	modifiedFiles := map[string][]byte{
		"main.py":    []byte("TAMPERED content"),
		"config.yaml": []byte("key: value"),
	}

	err := VerifyBundle(modifiedFiles, expectedHash)
	if err == nil {
		t.Fatal("VerifyBundle() expected error for modified file, got nil")
	}

	var integrityErr *BundleIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("expected *BundleIntegrityError, got %T: %v", err, err)
	}
	if integrityErr.ExpectedHash != expectedHash {
		t.Errorf("ExpectedHash = %q, want %q", integrityErr.ExpectedHash, expectedHash)
	}
	if integrityErr.ActualHash == expectedHash {
		t.Error("ActualHash should differ from expected hash when a file is modified")
	}
}
