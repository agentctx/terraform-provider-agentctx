---
page_title: "agentctx_subagent Resource"
subcategory: ""
description: |-
  Manages a Claude Code sub-agent definition file.
---

# agentctx_subagent (Resource)

Manages a Claude Code sub-agent definition file. Generates a Markdown file with YAML frontmatter that conforms to the [Claude Code sub-agent specification](https://code.claude.com/docs/en/sub-agents) and writes it to a local directory.

Sub-agents are specialized AI assistants that handle specific types of tasks. Each sub-agent runs in its own context window with a custom system prompt, specific tool access, and independent permissions.

## Example Usage

### Basic Code Reviewer

```hcl
resource "agentctx_subagent" "code_reviewer" {
  name        = "code-reviewer"
  description = "Expert code review specialist. Use after writing or modifying code."
  output_dir  = ".claude/agents"
  model       = "sonnet"
  tools       = ["Read", "Grep", "Glob", "Bash"]

  prompt = <<-EOT
    You are a senior code reviewer. When invoked, analyze the code and provide
    specific, actionable feedback on quality, security, and best practices.
  EOT
}
```

### Sub-agent with Hooks

```hcl
resource "agentctx_subagent" "db_reader" {
  name        = "db-reader"
  description = "Execute read-only database queries."
  output_dir  = ".claude/agents"
  tools       = ["Bash"]

  prompt = <<-EOT
    You are a database analyst with read-only access.
    Execute SELECT queries to answer questions about the data.
  EOT

  hooks {
    pre_tool_use {
      matcher = "Bash"
      hook {
        type    = "command"
        command = "./scripts/validate-readonly-query.sh"
      }
    }
  }
}
```

### Sub-agent with MCP Servers

```hcl
resource "agentctx_subagent" "slack_agent" {
  name        = "slack-agent"
  description = "Interact with Slack channels and messages."
  output_dir  = ".claude/agents"

  prompt = "You are a Slack assistant."

  mcp_server {
    name = "slack"
  }

  mcp_server {
    name    = "custom-api"
    command = "node"
    args    = ["server.js"]
    env     = {
      API_KEY = var.api_key
    }
  }
}
```

### Coordinator with Task Restrictions

```hcl
resource "agentctx_subagent" "coordinator" {
  name        = "coordinator"
  description = "Coordinates work across specialized agents"
  output_dir  = ".claude/agents"
  tools       = ["Task(worker, researcher)", "Read", "Bash"]
  skills      = ["api-conventions"]

  prompt = <<-EOT
    You are a project coordinator. Delegate tasks to the worker
    and researcher sub-agents as appropriate.
  EOT
}
```

## Argument Reference

### Required

- `name` (String) -- Unique identifier for the sub-agent. Must use lowercase letters and hyphens (e.g. `code-reviewer`). Changing this forces a new resource to be created.
- `description` (String) -- Describes when Claude should delegate to this sub-agent. Claude uses this description to decide automatic delegation.
- `output_dir` (String) -- Directory where the sub-agent markdown file will be written (e.g. `.claude/agents`). Changing this forces a new resource to be created.
- `prompt` (String) -- The system prompt for the sub-agent. This becomes the Markdown body after the YAML frontmatter.

### Optional

- `model` (String) -- Model the sub-agent uses. Valid values: `sonnet`, `opus`, `haiku`, `inherit`. Defaults to `inherit` if omitted.
- `tools` (List of String) -- Tools the sub-agent can use. Supports `Task(agent_type)` syntax for restricting spawnable sub-agents. Inherits all tools from the main conversation if omitted.
- `disallowed_tools` (List of String) -- Tools to deny, removed from the inherited or specified tool list.
- `permission_mode` (String) -- Controls how the sub-agent handles permission prompts. Valid values: `default`, `acceptEdits`, `delegate`, `dontAsk`, `bypassPermissions`, `plan`.
- `max_turns` (Number) -- Maximum number of agentic turns before the sub-agent stops.
- `skills` (List of String) -- Skills to preload into the sub-agent's context at startup. The full skill content is injected, not just made available for invocation.
- `memory` (String) -- Persistent memory scope for cross-session learning. Valid values: `user`, `project`, `local`.

### Blocks

#### `mcp_server`

Zero or more `mcp_server` blocks configure MCP servers available to this sub-agent.

- `name` (String, Required) -- Server name. If only `name` is set, it references an already-configured MCP server.
- `command` (String, Optional) -- Command to start the MCP server (for inline definitions).
- `args` (List of String, Optional) -- Arguments for the MCP server command.
- `env` (Map of String, Optional) -- Environment variables for the MCP server process.
- `url` (String, Optional) -- URL for a remote MCP server (SSE transport).

#### `hooks`

At most one `hooks` block configures lifecycle hooks scoped to the sub-agent.

##### `pre_tool_use`

Zero or more `pre_tool_use` blocks define hooks that run before the sub-agent uses a tool.

- `matcher` (String, Optional) -- Regex pattern to match tool names. Matches all tools if omitted.
- `hook` (Block, Required) -- One or more hook commands:
  - `type` (String, Required) -- Hook type. Currently only `command` is supported.
  - `command` (String, Required) -- Shell command to execute.

##### `post_tool_use`

Zero or more `post_tool_use` blocks define hooks that run after the sub-agent uses a tool. Same structure as `pre_tool_use`.

##### `stop`

Zero or more `stop` blocks define hooks that run when the sub-agent finishes. Same structure as `pre_tool_use`.

## Attribute Reference

In addition to all arguments above, the following attributes are exported:

- `id` (String) -- Unique identifier for the resource, derived from the output file path.
- `content` (String) -- The rendered Markdown content of the sub-agent file (YAML frontmatter + system prompt).
- `file_path` (String) -- Absolute path to the generated sub-agent markdown file.
- `content_hash` (String) -- SHA-256 hash of the rendered file content. Format: `sha256:{hex}`.

## Lifecycle Behavior

### Create

1. Renders the YAML frontmatter from resource attributes.
2. Combines frontmatter with the prompt to create a Markdown file.
3. Ensures the output directory exists and writes `{name}.md`.
4. Computes the content hash and saves all computed attributes to state.

### Read (Refresh)

1. Reads the file from disk at the stored `file_path`.
2. If the file no longer exists, removes the resource from state so Terraform plans recreation.
3. Updates `content` and `content_hash` from the file on disk to detect external modifications.

### Update

1. Re-renders the Markdown content with updated attributes.
2. Overwrites the existing file.
3. Updates all computed attributes in state.

### Destroy

1. Deletes the sub-agent markdown file from disk.
2. If the file was already deleted externally, the error is suppressed.

## Import

Import is not currently supported for this resource.
