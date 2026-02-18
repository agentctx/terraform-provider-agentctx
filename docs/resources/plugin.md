---
page_title: "agentctx_plugin Resource"
subcategory: ""
description: |-
  Manages a Claude Code plugin directory structure, including plugin.json, agents, skills, commands, hooks, MCP servers, LSP servers, and bundled files.
---

# agentctx_plugin (Resource)

Manages a Claude Code plugin directory structure. Generates the complete plugin layout including:

- `.claude-plugin/plugin.json` manifest
- `skills/` content
- `agents/` content
- `commands/` content
- `hooks/hooks.json`
- `.mcp.json` and `.lsp.json`
- Additional bundled files

This resource is designed for first-class Claude Code plugin authoring in Terraform, including composition with `agentctx_subagent` outputs.

## Example Usage

### Basic Plugin

```hcl
resource "agentctx_plugin" "deployment_tools" {
  name        = "deployment-tools"
  output_dir  = "${path.module}/plugins/deployment-tools"
  version     = "1.0.0"
  description = "Deployment automation tools"
  license     = "MIT"

  skill {
    name    = "deploy-conventions"
    content = <<-EOT
      # Deployment Conventions
      - Run tests before deploys
      - Verify health checks after rollout
    EOT
  }

  command {
    name    = "deploy"
    content = "Deploy the service to the selected environment."
  }
}
```

### Plugin Composed with Subagents, Hooks, MCP, and Files

```hcl
resource "agentctx_subagent" "security_reviewer" {
  name        = "security-reviewer"
  description = "Reviews code for security vulnerabilities"
  output_dir  = "${path.module}/.claude/agents"
  model       = "sonnet"
  tools       = ["Read", "Grep", "Glob"]
  prompt      = "You are a security specialist."
}

resource "agentctx_plugin" "enterprise" {
  name        = "enterprise-tools"
  output_dir  = "${path.module}/plugins/enterprise-tools"
  version     = "2.1.0"
  description = "Enterprise development automation"
  homepage    = "https://docs.example.com/enterprise-tools"
  repository  = "https://github.com/example/enterprise-tools"
  license     = "Apache-2.0"
  keywords    = ["enterprise", "security", "deployment"]

  author {
    name  = "Platform Team"
    email = "platform@example.com"
  }

  agent {
    name        = "security-reviewer"
    source_file = agentctx_subagent.security_reviewer.file_path
  }

  output_style {
    path = "styles/concise.md"
  }

  hooks {
    post_tool_use {
      matcher = "Write|Edit"
      hook {
        type    = "command"
        command = "$${CLAUDE_PLUGIN_ROOT}/scripts/lint.sh"
      }
    }
  }

  mcp_server {
    name    = "deploy-api"
    command = "$${CLAUDE_PLUGIN_ROOT}/servers/deploy-api"
    args    = ["--config", "$${CLAUDE_PLUGIN_ROOT}/config.json"]
  }

  lsp_server {
    name    = "custom-lang"
    command = "custom-language-server"
    args    = ["--stdio"]
    extension_to_language = {
      ".custom" = "customlang"
    }
    restart_on_crash = true
    max_restarts     = 3
  }

  file {
    path       = "scripts/lint.sh"
    content    = "#!/bin/bash\necho 'Running linter...'"
    executable = true
  }

  file {
    path    = "styles/concise.md"
    content = "# Concise style\n\nShort summary plus concrete next action."
  }
}
```

## Argument Reference

### Required

- `name` (String) -- Unique plugin identifier in kebab-case (`^[a-z0-9]+(-[a-z0-9]+)*$`). Changing this forces replacement.
- `output_dir` (String) -- Directory where the plugin structure is generated. Changing this forces replacement.

### Optional

- `version` (String) -- Semantic version string (for example `1.0.0`).
- `description` (String) -- Short plugin description.
- `homepage` (String) -- Plugin homepage or docs URL.
- `repository` (String) -- Source repository URL.
- `license` (String) -- License identifier such as `MIT` or `Apache-2.0`.
- `keywords` (List of String) -- Plugin discovery keywords.

### Blocks

#### `author`

At most one `author` block.

- `name` (String, Required) -- Author name.
- `email` (String, Optional) -- Author email.
- `url` (String, Optional) -- Author URL.

#### `output_style`

Zero or more style paths written to manifest `outputStyles`.

- `path` (String, Required) -- Relative path to a style file or directory in the plugin. Must be relative and must not contain `..`.

#### `skill`

Zero or more skills bundled into `skills/<name>/`.

- `name` (String, Required) -- Skill name (kebab-case).
- `source_dir` (String, Optional) -- Existing directory to copy into `skills/<name>/`.
- `content` (String, Optional) -- Inline `SKILL.md` content written to `skills/<name>/SKILL.md`.

~> Each `skill` block must set exactly one of `source_dir` or `content`.

#### `agent`

Zero or more agents bundled into `agents/`.

- `name` (String, Required) -- Agent name (kebab-case); file path is `agents/<name>.md`.
- `source_file` (String, Optional) -- Existing agent markdown file to copy.
- `content` (String, Optional) -- Inline agent markdown content.

~> Each `agent` block must set exactly one of `source_file` or `content`.

#### `command`

Zero or more slash commands bundled into `commands/`.

- `name` (String, Required) -- Command name (kebab-case); file path is `commands/<name>.md`.
- `source_file` (String, Optional) -- Existing command markdown file to copy.
- `content` (String, Optional) -- Inline command markdown content.

~> Each `command` block must set exactly one of `source_file` or `content`.

#### `mcp_server`

Zero or more MCP servers written to `.mcp.json`.

- `name` (String, Required) -- Server key name.
- `command` (String, Optional) -- Command for local/stdin-stdout transport.
- `url` (String, Optional) -- Remote SSE transport URL.
- `args` (List of String, Optional) -- Command arguments.
- `env` (Map of String, Optional) -- Command environment variables.
- `cwd` (String, Optional) -- Command working directory.

~> Exactly one of `command` or `url` must be set. If `url` is set, `args`, `env`, and `cwd` are not allowed.

#### `lsp_server`

Zero or more LSP servers written to `.lsp.json`.

- `name` (String, Required) -- LSP server key name.
- `command` (String, Required) -- LSP binary.
- `extension_to_language` (Map of String, Required) -- Extension to language mapping (`{ ".go" = "go" }`).
- `args` (List of String, Optional) -- Command arguments.
- `transport` (String, Optional) -- `stdio` or `socket`.
- `env` (Map of String, Optional) -- Environment variables.
- `initialization_options` (Map of String, Optional) -- Initialization options payload.
- `settings` (Map of String, Optional) -- Workspace settings payload.
- `workspace_folder` (String, Optional) -- Workspace folder path.
- `startup_timeout` (Number, Optional) -- Startup timeout in milliseconds.
- `shutdown_timeout` (Number, Optional) -- Shutdown timeout in milliseconds.
- `restart_on_crash` (Boolean, Optional) -- Auto-restart on crash. Defaults to `false`.
- `max_restarts` (Number, Optional) -- Max restart attempts.

#### `hooks`

At most one `hooks` block, written to `hooks/hooks.json`.

Supported event blocks:

- `pre_tool_use`
- `post_tool_use`
- `post_tool_use_failure`
- `permission_request`
- `user_prompt_submit`
- `notification`
- `stop`
- `subagent_start`
- `subagent_stop`
- `session_start`
- `session_end`
- `teammate_idle`
- `task_completed`
- `pre_compact`

Each event block contains one or more matcher entries:

- `matcher` (String, Optional) -- Regex matcher; omitted means all.
- `hook` (Block, Required) -- Hook actions:
  - `type` (String, Required) -- `command`, `prompt`, or `agent`.
  - `command` (String, Required) -- Hook command/prompt/agent payload.

#### `file`

Zero or more additional files written relative to plugin root.

- `path` (String, Required) -- Relative destination path (for example `scripts/lint.sh`). Must be relative and must not contain `..`.
- `content` (String, Optional) -- Inline file content.
- `source_file` (String, Optional) -- Existing local file to copy.
- `executable` (Boolean, Optional) -- Use executable mode (`0755`) when true. Defaults to `false`.

~> Each `file` block must set exactly one of `content` or `source_file`.

## Attribute Reference

In addition to all arguments above, the following attributes are exported:

- `id` (String) -- Absolute plugin root path, used as the Terraform resource ID.
- `plugin_dir` (String) -- Absolute plugin root path.
- `manifest_json` (String) -- Rendered `.claude-plugin/plugin.json` content.
- `content_hash` (String) -- SHA-256 hash of `manifest_json` in `sha256:{hex}` format.

## Lifecycle Behavior

### Create

1. Resolves `output_dir` to an absolute path.
2. Removes managed plugin artifacts (`.claude-plugin`, `skills`, `agents`, `commands`, `hooks`, `.mcp.json`, `.lsp.json`) to prevent stale content.
3. Rebuilds plugin directories/files from configuration blocks.
4. Writes `.claude-plugin/plugin.json`.
5. Stores `id`, `plugin_dir`, `manifest_json`, and `content_hash`.

### Read (Refresh)

1. Reads `.claude-plugin/plugin.json` from disk.
2. If the manifest is missing, removes the resource from Terraform state.
3. Recomputes `manifest_json` and `content_hash` from disk content.

### Update

1. Deletes extra files removed from `file` blocks.
2. Regenerates the plugin directory from the planned configuration.
3. Updates computed attributes in state.

### Destroy

1. Recursively deletes `plugin_dir`.
2. Suppresses not-found errors.

## Import

Import is not currently supported for this resource.
