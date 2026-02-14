package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
)

func TestAccSkill_WithAnthropic_FullLifecycle(t *testing.T) {
	acctest.SetupTest(t)

	mock := acctest.NewMockAnthropicServer(t)
	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "anthropic lifecycle test",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigWithAnthropic("primary", mock.URL()) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q

  anthropic {
    enabled = true
  }
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "id"),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
					resource.TestCheckResourceAttr("agentctx_skill.test", "target_states.%", "1"),
					resource.TestMatchResourceAttr("agentctx_skill.test", "registry_state.skill_id", regexp.MustCompile(`^skill_mock_`)),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "registry_state.deployed_version"),
				),
			},
		},
	})
}

func TestAccSkill_WithAnthropic_AutoVersion(t *testing.T) {
	acctest.SetupTest(t)

	mock := acctest.NewMockAnthropicServer(t)
	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "auto version v1",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigWithAnthropic("primary", mock.URL()) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q

  anthropic {
    enabled      = true
    auto_version = true
  }
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_skill.test", "registry_state.deployed_version", "v1"),
				),
			},
			{
				PreConfig: func() {
					if err := os.WriteFile(sourceDir+"/main.txt", []byte("auto version v2"), 0o644); err != nil {
						t.Fatalf("failed to update source file: %s", err)
					}
				},
				Config: acctest.ProviderConfigWithAnthropic("primary", mock.URL()) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q

  anthropic {
    enabled      = true
    auto_version = true
  }
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_skill.test", "registry_state.deployed_version", "v2"),
				),
			},
		},
	})
}

func TestAccSkill_VersionStrategy_PinnedRequiresVersion(t *testing.T) {
	acctest.SetupTest(t)

	mock := acctest.NewMockAnthropicServer(t)
	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "pinned test",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigWithAnthropic("primary", mock.URL()) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q

  anthropic {
    enabled          = true
    version_strategy = "pinned"
  }
}
`, sourceDir),
				ExpectError: regexp.MustCompile("pinned_version is required"),
			},
		},
	})
}

func TestAccSkill_VersionStrategy_ManualRequiresVersion(t *testing.T) {
	acctest.SetupTest(t)

	mock := acctest.NewMockAnthropicServer(t)
	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "manual test",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigWithAnthropic("primary", mock.URL()) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q

  anthropic {
    enabled          = true
    version_strategy = "manual"
  }
}
`, sourceDir),
				ExpectError: regexp.MustCompile("pinned_version is required"),
			},
		},
	})
}
