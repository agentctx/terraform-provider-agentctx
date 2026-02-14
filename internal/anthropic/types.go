package anthropic

// Skill represents a skill resource in the Anthropic API.
type Skill struct {
	ID            string `json:"id"`
	DisplayTitle  string `json:"display_title"`
	LatestVersion string `json:"latest_version,omitempty"`
	Source        string `json:"source,omitempty"`
	Type          string `json:"type,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// SkillVersion represents a deployed version of a skill.
type SkillVersion struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	SkillID     string `json:"skill_id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Directory   string `json:"directory,omitempty"`
	Type        string `json:"type,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// CreateSkillRequest is the request body for creating a new skill.
type CreateSkillRequest struct {
	DisplayTitle string `json:"display_title"`
}

// UpdateSkillRequest is the request body for updating an existing skill.
type UpdateSkillRequest struct {
	DisplayTitle string `json:"display_title"`
}

// ListSkillsResponse is the response body for listing skills.
type ListSkillsResponse struct {
	Data     []Skill `json:"data"`
	HasMore  bool    `json:"has_more"`
	NextPage string  `json:"next_page,omitempty"`
}

// ListVersionsResponse is the response body for listing skill versions.
type ListVersionsResponse struct {
	Data     []SkillVersion `json:"data"`
	HasMore  bool           `json:"has_more"`
	NextPage string         `json:"next_page,omitempty"`
}

// DeleteResponse is the response body for delete operations.
type DeleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// APIError represents an error response from the Anthropic API.
type APIError struct {
	StatusCode int
	Message    string `json:"message"`
	Type       string `json:"type"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Message != "" {
		return "anthropic: " + e.Type + ": " + e.Message
	}
	return "anthropic: HTTP " + httpStatusText(e.StatusCode)
}

// httpStatusText returns a short text representation for common HTTP status codes.
func httpStatusText(code int) string {
	switch code {
	case 400:
		return "400 Bad Request"
	case 401:
		return "401 Unauthorized"
	case 403:
		return "403 Forbidden"
	case 404:
		return "404 Not Found"
	case 409:
		return "409 Conflict"
	case 429:
		return "429 Too Many Requests"
	case 500:
		return "500 Internal Server Error"
	case 502:
		return "502 Bad Gateway"
	case 503:
		return "503 Service Unavailable"
	default:
		return string(rune('0'+code/100)) + "xx"
	}
}
