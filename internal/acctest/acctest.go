package acctest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/agentctx/terraform-provider-agentctx/internal/provider"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// TestProtoV6ProviderFactories is a map of provider factory functions
// suitable for use with the terraform-plugin-testing framework.
var TestProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"agentctx": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// SetupTest resets the global MemoryTarget registry so each test starts
// with a clean slate. Call this via t.Cleanup in every acceptance test.
func SetupTest(t *testing.T) {
	t.Helper()
	target.ResetMemoryTargets()
	t.Cleanup(func() {
		target.ResetMemoryTargets()
	})
}

// CreateTempSourceDir creates a temporary directory with the given files
// and returns the absolute path. The files map keys are relative paths and
// values are file contents. The directory is automatically cleaned up when
// the test finishes.
func CreateTempSourceDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("failed to create parent dir for %s: %s", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write file %s: %s", relPath, err)
		}
	}
	return dir
}

// ProviderConfigMemory returns an HCL snippet that configures the agentctx
// provider with a single memory target.
func ProviderConfigMemory(targetName string) string {
	return fmt.Sprintf(`
provider "agentctx" {
  target {
    name = %q
    type = "memory"
  }
}
`, targetName)
}

// ProviderConfigMemoryMulti returns an HCL snippet that configures the
// agentctx provider with multiple memory targets and optional default_targets.
func ProviderConfigMemoryMulti(names []string, defaults []string) string {
	var targets string
	for _, n := range names {
		targets += fmt.Sprintf(`
  target {
    name = %q
    type = "memory"
  }
`, n)
	}

	var defaultsHCL string
	if len(defaults) > 0 {
		defaultsHCL = "  default_targets = ["
		for i, d := range defaults {
			if i > 0 {
				defaultsHCL += ", "
			}
			defaultsHCL += fmt.Sprintf("%q", d)
		}
		defaultsHCL += "]\n"
	}

	return fmt.Sprintf(`
provider "agentctx" {
%s%s}
`, defaultsHCL, targets)
}

// ProviderConfigWithAnthropic returns an HCL snippet that configures the
// agentctx provider with a single memory target and an anthropic block
// pointing at the given mock server URL.
func ProviderConfigWithAnthropic(targetName string, mockURL string) string {
	return fmt.Sprintf(`
provider "agentctx" {
  target {
    name = %q
    type = "memory"
  }

  anthropic {
    api_key  = "test-api-key"
    base_url = %q
  }
}
`, targetName, mockURL)
}
