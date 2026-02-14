package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
)

func TestAccSkill_BasicLifecycle(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "hello world",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("primary") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "id"),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "source_hash"),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "skill_name"),
					resource.TestCheckResourceAttr("agentctx_skill.test", "target_states.%", "1"),
				),
			},
		},
	})
}

func TestAccSkill_Update(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "version 1",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("primary") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDir),
				Check: resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
			},
			{
				PreConfig: func() {
					if err := os.WriteFile(sourceDir+"/main.txt", []byte("version 2"), 0o644); err != nil {
						t.Fatalf("failed to update source file: %s", err)
					}
				},
				Config: acctest.ProviderConfigMemory("primary") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDir),
				Check: resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
			},
		},
	})
}

func TestAccSkill_MultiTarget(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "multi-target test",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemoryMulti(
					[]string{"alpha", "beta"},
					[]string{"alpha", "beta"},
				) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDir),
				Check: resource.TestCheckResourceAttr("agentctx_skill.test", "target_states.%", "2"),
			},
		},
	})
}

func TestAccSkill_ValidateOnly(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"index.js": "console.log('validate only')",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir    = %q
  validate_only = true
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_skill.test", "id", regexp.MustCompile(`^validate:`)),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
				),
			},
		},
	})
}

func TestAccSkill_ExcludePatterns(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt":  "keep me",
		"debug.log": "exclude me",
	})

	// Deploy without exclude, then with exclude — the hash should change.
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir    = %q
  validate_only = true
}
`, sourceDir),
				Check: resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
			},
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir    = %q
  validate_only = true
  exclude       = ["*.log"]
}
`, sourceDir),
				Check: resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
			},
		},
	})
}

func TestAccSkill_AmbiguousTargets_Error(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "test",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemoryMulti(
					[]string{"alpha", "beta"},
					nil, // no default_targets
				) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDir),
				ExpectError: regexp.MustCompile("Ambiguous Target Configuration"),
			},
		},
	})
}

func TestAccSkill_Tags(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "tagged skill",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
  tags = {
    environment = "test"
    team        = "platform"
  }
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_skill.test", "tags.environment", "test"),
					resource.TestCheckResourceAttr("agentctx_skill.test", "tags.team", "platform"),
				),
			},
		},
	})
}
