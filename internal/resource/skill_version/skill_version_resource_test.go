package skillversion

import (
	"errors"
	"fmt"
	"testing"

	"github.com/agentctx/terraform-provider-agentctx/internal/anthropic"
)

// ---------------------------------------------------------------------------
// isAPINotFound tests (fix #3: uses errors.As instead of type assertion)
// ---------------------------------------------------------------------------

func TestIsAPINotFound_DirectError(t *testing.T) {
	apiErr := &anthropic.APIError{StatusCode: 404, Type: "not_found_error", Message: "not found"}
	var target *anthropic.APIError

	if !isAPINotFound(apiErr, &target) {
		t.Error("expected isAPINotFound to return true for direct 404 *APIError")
	}
	if target != apiErr {
		t.Error("expected target to be set to the APIError")
	}
}

func TestIsAPINotFound_WrappedError(t *testing.T) {
	// Fix #3: errors.As should catch *APIError wrapped by fmt.Errorf.
	// The old code used a type assertion which would fail for wrapped errors.
	apiErr := &anthropic.APIError{StatusCode: 404, Type: "not_found_error", Message: "version not found"}
	wrapped := fmt.Errorf("get version failed: %w", apiErr)

	var target *anthropic.APIError
	if !isAPINotFound(wrapped, &target) {
		t.Error("expected isAPINotFound to return true for wrapped 404 *APIError")
	}
	if target == nil {
		t.Fatal("expected target to be non-nil")
	}
	if target.StatusCode != 404 {
		t.Errorf("target.StatusCode = %d, want 404", target.StatusCode)
	}
}

func TestIsAPINotFound_DoubleWrappedError(t *testing.T) {
	// Test deeply wrapped errors to ensure errors.As traverses the chain.
	apiErr := &anthropic.APIError{StatusCode: 404, Type: "not_found_error", Message: "deep"}
	wrapped1 := fmt.Errorf("inner: %w", apiErr)
	wrapped2 := fmt.Errorf("outer: %w", wrapped1)

	var target *anthropic.APIError
	if !isAPINotFound(wrapped2, &target) {
		t.Error("expected isAPINotFound to return true for double-wrapped 404 *APIError")
	}
	if target == nil {
		t.Fatal("expected target to be non-nil")
	}
}

func TestIsAPINotFound_Non404(t *testing.T) {
	apiErr := &anthropic.APIError{StatusCode: 400, Type: "invalid_request_error", Message: "bad request"}
	var target *anthropic.APIError

	if isAPINotFound(apiErr, &target) {
		t.Error("expected isAPINotFound to return false for 400 *APIError")
	}
}

func TestIsAPINotFound_NonAPIError(t *testing.T) {
	err := errors.New("some random error")
	var target *anthropic.APIError

	if isAPINotFound(err, &target) {
		t.Error("expected isAPINotFound to return false for non-APIError")
	}
}

func TestIsAPINotFound_NilError(t *testing.T) {
	var target *anthropic.APIError

	if isAPINotFound(nil, &target) {
		t.Error("expected isAPINotFound to return false for nil error")
	}
}

func TestIsAPINotFound_Wrapped500(t *testing.T) {
	// A wrapped 500 error should not be treated as "not found".
	apiErr := &anthropic.APIError{StatusCode: 500, Type: "server_error", Message: "internal error"}
	wrapped := fmt.Errorf("server issue: %w", apiErr)

	var target *anthropic.APIError
	if isAPINotFound(wrapped, &target) {
		t.Error("expected isAPINotFound to return false for wrapped 500 *APIError")
	}
}
