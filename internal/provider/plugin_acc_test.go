package provider_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
)

func TestAccPlugin_BasicLifecycle(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "basic-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
				return fmt.Errorf("plugin directory still exists after destroy: %s", outputDir)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "basic-plugin"
  output_dir = %q
  version    = "1.0.0"
  description = "A basic test plugin"
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_plugin.test", "id"),
					resource.TestCheckResourceAttrSet("agentctx_plugin.test", "plugin_dir"),
					resource.TestCheckResourceAttrSet("agentctx_plugin.test", "manifest_json"),
					resource.TestCheckResourceAttrSet("agentctx_plugin.test", "content_hash"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "name", "basic-plugin"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "version", "1.0.0"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "description", "A basic test plugin"),
					// Verify manifest JSON contains the name
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`"name":\s*"basic-plugin"`)),
				),
			},
		},
	})
}

func TestAccPlugin_WithAuthor(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "author-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "author-plugin"
  output_dir = %q

  author {
    name  = "Test Author"
    email = "test@example.com"
    url   = "https://github.com/test"
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`"author"`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`Test Author`)),
				),
			},
		},
	})
}

func TestAccPlugin_WithInlineSkill(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "skill-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "skill-plugin"
  output_dir = %q

  skill {
    name    = "code-reviewer"
    content = "# Code Reviewer\n\nReview code for quality."
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`skills`)),
					// Verify file on disk
					func(s *terraform.State) error {
						skillPath := filepath.Join(outputDir, "skills", "code-reviewer", "SKILL.md")
						data, err := os.ReadFile(skillPath)
						if err != nil {
							return fmt.Errorf("failed to read skill file: %w", err)
						}
						if string(data) != "# Code Reviewer\n\nReview code for quality." {
							return fmt.Errorf("unexpected skill content: %q", string(data))
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_WithSourceDirSkill(t *testing.T) {
	acctest.SetupTest(t)

	// Create a source skill directory
	srcDir := acctest.CreateTempSourceDir(t, map[string]string{
		"SKILL.md":     "# Copied Skill\n\nA skill from source.",
		"reference.md": "# Reference\n\nAdditional docs.",
	})

	outputDir := filepath.Join(t.TempDir(), "srcdir-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "srcdir-plugin"
  output_dir = %q

  skill {
    name       = "copied-skill"
    source_dir = %q
  }
}
`, outputDir, srcDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						// Verify both files were copied
						skillDir := filepath.Join(outputDir, "skills", "copied-skill")
						for _, file := range []string{"SKILL.md", "reference.md"} {
							if _, err := os.Stat(filepath.Join(skillDir, file)); os.IsNotExist(err) {
								return fmt.Errorf("expected %s to exist in copied skill", file)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_WithAgentFromSubagent(t *testing.T) {
	acctest.SetupTest(t)

	agentDir := filepath.Join(t.TempDir(), "agents")
	outputDir := filepath.Join(t.TempDir(), "agent-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "reviewer" {
  name        = "security-reviewer"
  description = "Reviews code for security issues"
  output_dir  = %q
  prompt      = "You are a security specialist."
}

resource "agentctx_plugin" "test" {
  name       = "agent-plugin"
  output_dir = %q

  agent {
    name        = "security-reviewer"
    source_file = agentctx_subagent.reviewer.file_path
  }
}
`, agentDir, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`agents`)),
					func(s *terraform.State) error {
						agentPath := filepath.Join(outputDir, "agents", "security-reviewer.md")
						data, err := os.ReadFile(agentPath)
						if err != nil {
							return fmt.Errorf("failed to read agent file: %w", err)
						}
						if !regexp.MustCompile(`security-reviewer`).Match(data) {
							return fmt.Errorf("agent file missing expected content")
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_WithHooksAndMcp(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "hooks-mcp-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "hooks-mcp-plugin"
  output_dir = %q

  hooks {
    post_tool_use {
      matcher = "Write|Edit"
      hook {
        type    = "command"
        command = "${CLAUDE_PLUGIN_ROOT}/scripts/format.sh"
      }
    }
    stop {
      hook {
        type    = "command"
        command = "${CLAUDE_PLUGIN_ROOT}/scripts/cleanup.sh"
      }
    }
  }

  mcp_server {
    name    = "plugin-db"
    command = "${CLAUDE_PLUGIN_ROOT}/servers/db"
    args    = ["--port", "8080"]
    env     = {
      DB_PATH = "${CLAUDE_PLUGIN_ROOT}/data"
    }
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`hooks`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`mcpServers`)),
					func(s *terraform.State) error {
						// Verify hooks.json
						hooksPath := filepath.Join(outputDir, "hooks", "hooks.json")
						data, err := os.ReadFile(hooksPath)
						if err != nil {
							return fmt.Errorf("failed to read hooks.json: %w", err)
						}
						var hooks map[string]interface{}
						if err := json.Unmarshal(data, &hooks); err != nil {
							return fmt.Errorf("invalid hooks JSON: %w", err)
						}

						// Verify .mcp.json
						mcpPath := filepath.Join(outputDir, ".mcp.json")
						data, err = os.ReadFile(mcpPath)
						if err != nil {
							return fmt.Errorf("failed to read .mcp.json: %w", err)
						}
						var mcp map[string]interface{}
						if err := json.Unmarshal(data, &mcp); err != nil {
							return fmt.Errorf("invalid MCP JSON: %w", err)
						}
						if _, ok := mcp["mcpServers"]; !ok {
							return fmt.Errorf("expected mcpServers key in .mcp.json")
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_WithLspServer(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "lsp-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "lsp-plugin"
  output_dir = %q

  lsp_server {
    name    = "go"
    command = "gopls"
    args    = ["serve"]
    extension_to_language = {
      ".go" = "go"
    }
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`lspServers`)),
					func(s *terraform.State) error {
						lspPath := filepath.Join(outputDir, ".lsp.json")
						data, err := os.ReadFile(lspPath)
						if err != nil {
							return fmt.Errorf("failed to read .lsp.json: %w", err)
						}
						var lsp map[string]interface{}
						if err := json.Unmarshal(data, &lsp); err != nil {
							return fmt.Errorf("invalid LSP JSON: %w", err)
						}
						goConfig, ok := lsp["go"].(map[string]interface{})
						if !ok {
							return fmt.Errorf("expected 'go' key in .lsp.json")
						}
						if goConfig["command"] != "gopls" {
							return fmt.Errorf("expected gopls command, got %v", goConfig["command"])
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_WithExtraFiles(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "files-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "files-plugin"
  output_dir = %q

  file {
    path       = "scripts/format.sh"
    content    = "#!/bin/bash\necho formatting"
    executable = true
  }

  file {
    path    = "config/defaults.json"
    content = "{\"key\": \"value\"}"
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						// Verify script is executable
						scriptPath := filepath.Join(outputDir, "scripts", "format.sh")
						info, err := os.Stat(scriptPath)
						if err != nil {
							return fmt.Errorf("script not found: %w", err)
						}
						if info.Mode().Perm()&0o111 == 0 {
							return fmt.Errorf("expected executable permission on script")
						}

						// Verify config exists
						configPath := filepath.Join(outputDir, "config", "defaults.json")
						if _, err := os.Stat(configPath); os.IsNotExist(err) {
							return fmt.Errorf("config file not found")
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_Update(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "update-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name        = "update-plugin"
  output_dir  = %q
  version     = "1.0.0"
  description = "Original description"
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_plugin.test", "version", "1.0.0"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "description", "Original description"),
				),
			},
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name        = "update-plugin"
  output_dir  = %q
  version     = "2.0.0"
  description = "Updated description"
  license     = "MIT"

  skill {
    name    = "new-skill"
    content = "# New Skill"
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_plugin.test", "version", "2.0.0"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "description", "Updated description"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "license", "MIT"),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`new-skill`)),
				),
			},
		},
	})
}

func TestAccPlugin_FullFeatured(t *testing.T) {
	acctest.SetupTest(t)

	agentDir := filepath.Join(t.TempDir(), "agents")
	outputDir := filepath.Join(t.TempDir(), "full-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "reviewer" {
  name        = "code-reviewer"
  description = "Reviews code for quality"
  output_dir  = %q
  prompt      = "You are a code reviewer."
  model       = "sonnet"
}

resource "agentctx_plugin" "test" {
  name        = "enterprise-tools"
  output_dir  = %q
  version     = "2.1.0"
  description = "Enterprise deployment automation"
  homepage    = "https://docs.example.com"
  repository  = "https://github.com/example/tools"
  license     = "MIT"
  keywords    = ["deployment", "ci-cd"]

  author {
    name  = "Dev Team"
    email = "dev@example.com"
  }

  skill {
    name    = "api-conventions"
    content = "# API Conventions\n\nFollow REST API best practices."
  }

  agent {
    name        = "code-reviewer"
    source_file = agentctx_subagent.reviewer.file_path
  }

  agent {
    name    = "security-checker"
    content = "---\nname: security-checker\ndescription: Security specialist\n---\n\nYou check for security issues."
  }

  command {
    name    = "deploy"
    content = "Deploy the application to production."
  }

  hooks {
    post_tool_use {
      matcher = "Write|Edit"
      hook {
        type    = "command"
        command = "${CLAUDE_PLUGIN_ROOT}/scripts/lint.sh"
      }
    }
  }

  mcp_server {
    name    = "deploy-server"
    command = "${CLAUDE_PLUGIN_ROOT}/servers/deploy"
    args    = ["--port", "3000"]
  }

  lsp_server {
    name    = "typescript"
    command = "typescript-language-server"
    args    = ["--stdio"]
    extension_to_language = {
      ".ts"  = "typescript"
      ".tsx" = "typescriptreact"
    }
  }

  file {
    path       = "scripts/lint.sh"
    content    = "#!/bin/bash\necho 'linting'"
    executable = true
  }
}
`, agentDir, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_plugin.test", "name", "enterprise-tools"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "version", "2.1.0"),
					resource.TestCheckResourceAttr("agentctx_plugin.test", "license", "MIT"),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`enterprise-tools`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`skills`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`agents`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`commands`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`hooks`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`mcpServers`)),
					resource.TestMatchResourceAttr("agentctx_plugin.test", "manifest_json", regexp.MustCompile(`lspServers`)),
					func(s *terraform.State) error {
						// Verify all files exist on disk
						files := []string{
							".claude-plugin/plugin.json",
							"skills/api-conventions/SKILL.md",
							"agents/code-reviewer.md",
							"agents/security-checker.md",
							"commands/deploy.md",
							"hooks/hooks.json",
							".mcp.json",
							".lsp.json",
							"scripts/lint.sh",
						}
						for _, f := range files {
							path := filepath.Join(outputDir, f)
							if _, err := os.Stat(path); os.IsNotExist(err) {
								return fmt.Errorf("expected file to exist: %s", f)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccPlugin_InvalidName(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "invalid-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name       = "Invalid_Plugin"
  output_dir = %q
}
`, outputDir),
				ExpectError: regexp.MustCompile(`must contain only lowercase letters`),
			},
		},
	})
}

func TestAccPlugin_VerifyManifestOnDisk(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := filepath.Join(t.TempDir(), "verify-plugin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_plugin" "test" {
  name        = "verify-plugin"
  output_dir  = %q
  version     = "1.0.0"
  description = "Verify on disk"
  license     = "Apache-2.0"
  keywords    = ["test", "verify"]
}
`, outputDir),
				Check: func(s *terraform.State) error {
					manifestPath := filepath.Join(outputDir, ".claude-plugin", "plugin.json")
					data, err := os.ReadFile(manifestPath)
					if err != nil {
						return fmt.Errorf("failed to read manifest: %w", err)
					}

					var manifest map[string]interface{}
					if err := json.Unmarshal(data, &manifest); err != nil {
						return fmt.Errorf("invalid manifest JSON: %w", err)
					}

					if manifest["name"] != "verify-plugin" {
						return fmt.Errorf("expected name 'verify-plugin', got %v", manifest["name"])
					}
					if manifest["version"] != "1.0.0" {
						return fmt.Errorf("expected version '1.0.0', got %v", manifest["version"])
					}
					if manifest["license"] != "Apache-2.0" {
						return fmt.Errorf("expected license 'Apache-2.0', got %v", manifest["license"])
					}

					keywords, ok := manifest["keywords"].([]interface{})
					if !ok || len(keywords) != 2 {
						return fmt.Errorf("expected 2 keywords, got %v", manifest["keywords"])
					}

					return nil
				},
			},
		},
	})
}
