package provider_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
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

func TestAccSkill_RemoveTarget_CleansUpRemovedTarget(t *testing.T) {
	acctest.SetupTest(t)

	sourceDir := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "target-removal test",
	})
	skillName := filepath.Base(sourceDir)
	skillPrefix := skillName + "/.agentctx/"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemoryMulti(
					[]string{"alpha", "beta"},
					nil,
				) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
  targets    = ["alpha", "beta"]
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_skill.test", "target_states.%", "2"),
					func(_ *terraform.State) error {
						beta := target.GetOrCreateMemoryTarget("beta")
						objs, err := beta.List(context.Background(), skillPrefix)
						if err != nil {
							return fmt.Errorf("list beta objects: %w", err)
						}
						if len(objs) == 0 {
							return fmt.Errorf("expected beta to have deployed objects under %q", skillPrefix)
						}
						return nil
					},
				),
			},
			{
				Config: acctest.ProviderConfigMemoryMulti(
					[]string{"alpha", "beta"},
					nil,
				) + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
  targets    = ["alpha"]
}
`, sourceDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_skill.test", "target_states.%", "1"),
					func(_ *terraform.State) error {
						beta := target.GetOrCreateMemoryTarget("beta")
						objs, err := beta.List(context.Background(), skillPrefix)
						if err != nil {
							return fmt.Errorf("list beta objects: %w", err)
						}
						if len(objs) != 0 {
							return fmt.Errorf("expected beta deployment to be destroyed, found %d remaining objects", len(objs))
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccSkill_SourceDirChange_CleansUpOldSkillPrefix(t *testing.T) {
	acctest.SetupTest(t)

	sourceDirV1 := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "source-dir-v1",
	})
	sourceDirV2 := acctest.CreateTempSourceDir(t, map[string]string{
		"main.txt": "source-dir-v2",
	})

	oldPrefix := filepath.Base(sourceDirV1) + "/.agentctx/"
	newPrefix := filepath.Base(sourceDirV2) + "/.agentctx/"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("alpha") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDirV1),
				Check: func(_ *terraform.State) error {
					alpha := target.GetOrCreateMemoryTarget("alpha")
					objs, err := alpha.List(context.Background(), oldPrefix)
					if err != nil {
						return fmt.Errorf("list v1 objects: %w", err)
					}
					if len(objs) == 0 {
						return fmt.Errorf("expected v1 deployment under %q", oldPrefix)
					}
					return nil
				},
			},
			{
				Config: acctest.ProviderConfigMemory("alpha") + fmt.Sprintf(`
resource "agentctx_skill" "test" {
  source_dir = %q
}
`, sourceDirV2),
				Check: resource.ComposeAggregateTestCheckFunc(
					func(_ *terraform.State) error {
						alpha := target.GetOrCreateMemoryTarget("alpha")

						oldObjs, err := alpha.List(context.Background(), oldPrefix)
						if err != nil {
							return fmt.Errorf("list old prefix: %w", err)
						}
						if len(oldObjs) != 0 {
							return fmt.Errorf("expected old prefix %q to be removed, found %d objects", oldPrefix, len(oldObjs))
						}

						newObjs, err := alpha.List(context.Background(), newPrefix)
						if err != nil {
							return fmt.Errorf("list new prefix: %w", err)
						}
						if len(newObjs) == 0 {
							return fmt.Errorf("expected new deployment under %q", newPrefix)
						}

						return nil
					},
				),
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
