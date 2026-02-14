package acctest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSkill represents a skill in the mock registry.
type mockSkill struct {
	ID           string `json:"id"`
	DisplayTitle string `json:"display_title"`
	Source       string `json:"source,omitempty"`
	Type         string `json:"type,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// mockVersion represents a skill version in the mock registry.
type mockVersion struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	SkillID   string `json:"skill_id"`
	CreatedAt string `json:"created_at"`
}

// MockAnthropicServer holds the state for the mock Anthropic API server.
type MockAnthropicServer struct {
	mu            sync.Mutex
	skills        map[string]*mockSkill
	versions      map[string][]*mockVersion // keyed by skill ID
	skillCounter  int
	versionCounts map[string]int // per-skill version counter
	Server        *httptest.Server
}

// NewMockAnthropicServer creates a new mock Anthropic API server and returns
// it. Call Close() when done (typically via t.Cleanup).
func NewMockAnthropicServer(t *testing.T) *MockAnthropicServer {
	t.Helper()

	m := &MockAnthropicServer{
		skills:        make(map[string]*mockSkill),
		versions:      make(map[string][]*mockVersion),
		versionCounts: make(map[string]int),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/skills", m.handleSkills)
	mux.HandleFunc("/v1/skills/", m.handleSkillsWithID)

	m.Server = httptest.NewServer(mux)
	t.Cleanup(m.Server.Close)

	return m
}

// URL returns the base URL of the mock server.
func (m *MockAnthropicServer) URL() string {
	return m.Server.URL
}

func (m *MockAnthropicServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		m.createSkill(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *MockAnthropicServer) handleSkillsWithID(w http.ResponseWriter, r *http.Request) {
	// Parse: /v1/skills/{id}[/versions[/{version}]]
	path := strings.TrimPrefix(r.URL.Path, "/v1/skills/")
	parts := strings.SplitN(path, "/", 3)

	skillID := parts[0]

	if len(parts) == 1 {
		// /v1/skills/{id}
		switch r.Method {
		case http.MethodGet:
			m.getSkill(w, skillID)
		case http.MethodPut:
			m.updateSkill(w, r, skillID)
		case http.MethodDelete:
			m.deleteSkill(w, skillID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if parts[1] != "versions" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if len(parts) == 2 {
		// /v1/skills/{id}/versions
		switch r.Method {
		case http.MethodPost:
			m.createVersion(w, r, skillID)
		case http.MethodGet:
			m.listVersions(w, skillID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /v1/skills/{id}/versions/{version}
	versionStr := parts[2]
	switch r.Method {
	case http.MethodGet:
		m.getVersion(w, skillID, versionStr)
	case http.MethodDelete:
		m.deleteVersion(w, skillID, versionStr)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *MockAnthropicServer) createSkill(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Parse multipart form to extract display_title
	_ = r.ParseMultipartForm(32 << 20)
	displayTitle := r.FormValue("display_title")
	if displayTitle == "" {
		displayTitle = "Untitled Skill"
	}

	m.skillCounter++
	id := fmt.Sprintf("skill_mock_%03d", m.skillCounter)
	now := time.Now().UTC().Format(time.RFC3339)

	skill := &mockSkill{
		ID:           id,
		DisplayTitle: displayTitle,
		Type:         "skill",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.skills[id] = skill

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(skill)
}

func (m *MockAnthropicServer) getSkill(w http.ResponseWriter, skillID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[skillID]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Skill %q not found", skillID))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(skill)
}

func (m *MockAnthropicServer) updateSkill(w http.ResponseWriter, r *http.Request, skillID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[skillID]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Skill %q not found", skillID))
		return
	}

	var req struct {
		DisplayTitle string `json:"display_title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON body")
		return
	}

	if req.DisplayTitle != "" {
		skill.DisplayTitle = req.DisplayTitle
	}
	skill.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(skill)
}

func (m *MockAnthropicServer) deleteSkill(w http.ResponseWriter, skillID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.skills[skillID]; !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Skill %q not found", skillID))
		return
	}

	delete(m.skills, skillID)
	delete(m.versions, skillID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":   skillID,
		"type": "deleted",
	})
}

func (m *MockAnthropicServer) createVersion(w http.ResponseWriter, r *http.Request, skillID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.skills[skillID]; !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Skill %q not found", skillID))
		return
	}

	// Parse multipart form (files are sent but we don't need to inspect them)
	_ = r.ParseMultipartForm(32 << 20)

	m.versionCounts[skillID]++
	vNum := m.versionCounts[skillID]
	versionStr := fmt.Sprintf("v%d", vNum)
	versionID := fmt.Sprintf("ver_mock_%s_%03d", skillID, vNum)
	now := time.Now().UTC().Format(time.RFC3339)

	ver := &mockVersion{
		ID:        versionID,
		Version:   versionStr,
		SkillID:   skillID,
		CreatedAt: now,
	}
	m.versions[skillID] = append(m.versions[skillID], ver)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ver)
}

func (m *MockAnthropicServer) listVersions(w http.ResponseWriter, skillID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.skills[skillID]; !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Skill %q not found", skillID))
		return
	}

	versions := m.versions[skillID]
	if versions == nil {
		versions = []*mockVersion{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":     versions,
		"has_more": false,
	})
}

func (m *MockAnthropicServer) getVersion(w http.ResponseWriter, skillID, versionStr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	versions := m.versions[skillID]
	for _, v := range versions {
		if v.Version == versionStr {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(v)
			return
		}
	}

	writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Version %q not found for skill %q", versionStr, skillID))
}

func (m *MockAnthropicServer) deleteVersion(w http.ResponseWriter, skillID, versionStr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	versions := m.versions[skillID]
	for i, v := range versions {
		if v.Version == versionStr {
			m.versions[skillID] = append(versions[:i], versions[i+1:]...)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"id":   v.ID,
				"type": "deleted",
			})
			return
		}
	}

	writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("Version %q not found", versionStr))
}

func writeAPIError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	})
}
