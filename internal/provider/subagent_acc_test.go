package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/agentctx/terraform-provider-agentctx/internal/acctest"
)

func TestAccSubagent_BasicLifecycle(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			// Verify the file was deleted on destroy.
			fp := outputDir + "/code-reviewer.md"
			if _, err := os.Stat(fp); !os.IsNotExist(err) {
				return fmt.Errorf("sub-agent file still exists after destroy: %s", fp)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "code-reviewer"
  description = "Reviews code for quality"
  output_dir  = %q
  prompt      = "You are a code reviewer."
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "id"),
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "content"),
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "file_path"),
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "content_hash"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "name", "code-reviewer"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "description", "Reviews code for quality"),
				),
			},
		},
	})
}

func TestAccSubagent_AllFields(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name             = "full-agent"
  description      = "A fully configured agent"
  output_dir       = %q
  prompt           = "You are a specialized agent."
  model            = "sonnet"
  tools            = ["Read", "Grep", "Glob", "Bash"]
  disallowed_tools = ["Write", "Edit"]
  permission_mode  = "acceptEdits"
  max_turns        = 50
  skills           = ["api-conventions", "error-handling"]
  memory           = "user"
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "content"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "name", "full-agent"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "model", "sonnet"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "permission_mode", "acceptEdits"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "max_turns", "50"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "memory", "user"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "tools.#", "4"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "disallowed_tools.#", "2"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "skills.#", "2"),
				),
			},
		},
	})
}

func TestAccSubagent_WithHooks(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "hooked-agent"
  description = "Agent with hooks"
  output_dir  = %q
  prompt      = "You are an agent with hooks."
  tools       = ["Bash"]

  hooks {
    pre_tool_use {
      matcher = "Bash"
      hook {
        type    = "command"
        command = "./scripts/validate.sh"
      }
    }
    post_tool_use {
      matcher = "Edit|Write"
      hook {
        type    = "command"
        command = "./scripts/lint.sh"
      }
    }
    stop {
      hook {
        type    = "command"
        command = "./scripts/cleanup.sh"
      }
    }
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "content"),
					// Verify the rendered content contains the hook configuration
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`PreToolUse`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`PostToolUse`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`validate\.sh`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`lint\.sh`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`cleanup\.sh`)),
				),
			},
		},
	})
}

func TestAccSubagent_WithMcpServers(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "mcp-agent"
  description = "Agent with MCP servers"
  output_dir  = %q
  prompt      = "You are an agent with MCP."

  mcp_server {
    name = "slack"
  }

  mcp_server {
    name    = "custom-server"
    command = "node"
    args    = ["server.js"]
    env     = {
      API_KEY = "test-key"
    }
  }
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("agentctx_subagent.test", "content"),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`mcpServers`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`slack`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`custom-server`)),
				),
			},
		},
	})
}

func TestAccSubagent_Update(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "updatable-agent"
  description = "Original description"
  output_dir  = %q
  prompt      = "Original prompt."
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_subagent.test", "description", "Original description"),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`Original prompt`)),
				),
			},
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "updatable-agent"
  description = "Updated description"
  output_dir  = %q
  prompt      = "Updated prompt."
  model       = "haiku"
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("agentctx_subagent.test", "description", "Updated description"),
					resource.TestCheckResourceAttr("agentctx_subagent.test", "model", "haiku"),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`Updated prompt`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`model: haiku`)),
				),
			},
		},
	})
}

func TestAccSubagent_TaskToolSyntax(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "coordinator"
  description = "Coordinates work across agents"
  output_dir  = %q
  prompt      = "You are a coordinator."
  tools       = ["Task(worker, researcher)", "Read", "Bash"]
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`tools: Task\(worker, researcher\), Read, Bash`)),
				),
			},
		},
	})
}

func TestAccSubagent_FileContent(t *testing.T) {
	acctest.SetupTest(t)

	outputDir := t.TempDir()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.TestProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigMemory("test") + fmt.Sprintf(`
resource "agentctx_subagent" "test" {
  name        = "test-agent"
  description = "Test agent"
  output_dir  = %q
  prompt      = "You are a test agent.\n\nFollow these instructions."
  model       = "sonnet"
  tools       = ["Read", "Grep"]
}
`, outputDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify file starts with YAML frontmatter
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`^---\n`)),
					// Verify frontmatter closes and prompt follows
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`---\n\nYou are a test agent`)),
					// Verify YAML field names match Claude Code spec (camelCase)
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`(?m)^name: test-agent$`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`(?m)^description: Test agent$`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`(?m)^model: sonnet$`)),
					resource.TestMatchResourceAttr("agentctx_subagent.test", "content", regexp.MustCompile(`(?m)^tools: Read, Grep$`)),
				),
			},
		},
	})
}
