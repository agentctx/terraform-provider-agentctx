package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
)

func TestAccProvider_MissingTargets(t *testing.T) {
	acctest.SetupTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "agentctx" {
}

resource "agentctx_skill" "test" {
  source_dir = "/tmp/nonexistent"
}
`,
				ExpectError: regexp.MustCompile("Missing Target Configuration"),
			},
		},
	})
}

func TestAccProvider_DuplicateTargetNames(t *testing.T) {
	acctest.SetupTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "agentctx" {
  target {
    name = "dupe"
    type = "memory"
  }
  target {
    name = "dupe"
    type = "memory"
  }
}

resource "agentctx_skill" "test" {
  source_dir = "/tmp/nonexistent"
}
`,
				ExpectError: regexp.MustCompile("Duplicate Target Name"),
			},
		},
	})
}

func TestAccProvider_InvalidDefaultTargets(t *testing.T) {
	acctest.SetupTest(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "agentctx" {
  default_targets = ["nonexistent"]

  target {
    name = "real"
    type = "memory"
  }
}

resource "agentctx_skill" "test" {
  source_dir = "/tmp/nonexistent"
}
`,
				ExpectError: regexp.MustCompile("Invalid Default Target"),
			},
		},
	})
}

func TestAccProvider_MemoryTargetWorks(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "hello world",
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + `
resource "agentctx_skill" "test" {
  source_dir    = "` + sourceDir + `"
  validate_only = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "id"),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "bundle_hash"),
					resource.TestCheckResourceAttrSet("agentctx_skill.test", "skill_name"),
				),
			},
		},
	})
}
