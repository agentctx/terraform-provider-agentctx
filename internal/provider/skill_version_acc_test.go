package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
)

func TestAccSkillVersion_BasicLifecycle(t *testing.T) {
	acctest.SetupTest(t)

	mock := acctest.NewMockAnthropicServer(t)
	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "version resource test",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigWithAnthropic("primary", mock.URL()) + fmt.Sprintf(`
resource "agentctx_skill" "parent" {
  source_dir = %q

  anthropic {
    enabled      = true
    auto_version = false
  }
}

resource "agentctx_skill_version" "test" {
  skill_id   = agentctx_skill.parent.registry_state.skill_id
  source_dir = %q
}
`, sourceDir, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_skill_version.test", "id"),
					resource.TestCheckResourceAttrSet("agentctx_skill_version.test", "version"),
					resource.TestCheckResourceAttrSet("agentctx_skill_version.test", "bundle_hash"),
					resource.TestCheckResourceAttrSet("agentctx_skill_version.test", "created_at"),
				),
			},
		},
	})
}

func TestAccSkillVersion_MissingAnthropicBlock_Error(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "no anthropic",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_skill_version" "test" {
  skill_id   = "skill_fake_001"
  source_dir = %q
}
`, sourceDir),
				ExpectError: regexp.MustCompile("Anthropic Client Not Configured"),
			},
		},
	})
}
