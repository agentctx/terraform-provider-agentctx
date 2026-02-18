# Basic plugin with inline skill and agent
resource "agentctx_plugin" "basic" {
  name        = "deployment-tools"
  output_dir  = "${path.module}/plugins/deployment-tools"
  version     = "1.0.0"
  description = "Deployment automation tools"
  license     = "MIT"

  skill {
    name    = "deploy-conventions"
    content = <<-EOT
      # Deployment Conventions

      Follow these deployment standards:
      - Always run tests before deploying
      - Use blue-green deployment strategy
      - Verify health checks after deployment
    EOT
  }

  agent {
    name    = "deployment-checker"
    content = <<-EOT
      ---
      name: deployment-checker
      description: Verifies deployment readiness and health
      ---

      You are a deployment specialist. Check that all prerequisites
      are met before allowing a deployment to proceed.
    EOT
  }

  command {
    name    = "deploy"
    content = "Deploy the application to the specified environment."
  }
}

# Plugin that references skills and subagents from other resources
resource "agentctx_subagent" "security_reviewer" {
  name        = "security-reviewer"
  description = "Reviews code for security vulnerabilities"
  output_dir  = "${path.module}/.claude/agents"
  model       = "sonnet"
  tools       = ["Read", "Grep", "Glob"]

  prompt = <<-EOT
    You are a security specialist. Review code for OWASP Top 10
    vulnerabilities and other security issues.
  EOT
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

  # Reference a skill directory managed separately
  skill {
    name       = "api-conventions"
    source_dir = "${path.module}/skills/api-conventions"
  }

  # Inline skill
  skill {
    name    = "error-handling"
    content = "# Error Handling Patterns\n\nUse structured error types."
  }

  # Reference a subagent managed by agentctx_subagent
  agent {
    name        = "security-reviewer"
    source_file = agentctx_subagent.security_reviewer.file_path
  }

  command {
    name    = "status"
    content = "Show current deployment status and health checks."
  }

  output_style {
    path = "styles/concise.md"
  }

  output_style {
    path = "styles/detailed.md"
  }

  # Event hooks using $${CLAUDE_PLUGIN_ROOT} for portable paths
  hooks {
    post_tool_use {
      matcher = "Write|Edit"
      hook {
        type    = "command"
        command = "$${CLAUDE_PLUGIN_ROOT}/scripts/lint.sh"
      }
    }
    session_start {
      hook {
        type    = "command"
        command = "$${CLAUDE_PLUGIN_ROOT}/scripts/setup.sh"
      }
    }
  }

  # Bundled MCP server
  mcp_server {
    name    = "deploy-api"
    command = "$${CLAUDE_PLUGIN_ROOT}/servers/deploy-api"
    args    = ["--config", "$${CLAUDE_PLUGIN_ROOT}/config.json"]
    env = {
      LOG_LEVEL = "info"
    }
  }

  # LSP server for custom language support
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

  # Bundled scripts
  file {
    path       = "scripts/lint.sh"
    content    = "#!/bin/bash\necho 'Running linter...'"
    executable = true
  }

  file {
    path       = "scripts/setup.sh"
    content    = "#!/bin/bash\necho 'Setting up environment...'"
    executable = true
  }

  file {
    path    = "config.json"
    content = jsonencode({ port = 3000, debug = false })
  }

  file {
    path    = "styles/concise.md"
    content = "# Concise output style\n\nRespond with a short summary and explicit next action."
  }

  file {
    path    = "styles/detailed.md"
    content = "# Detailed output style\n\nRespond with context, reasoning, and a remediation checklist."
  }
}

output "basic_plugin_dir" {
  value = agentctx_plugin.basic.plugin_dir
}

output "enterprise_plugin_dir" {
  value = agentctx_plugin.enterprise.plugin_dir
}

output "enterprise_manifest" {
  value = agentctx_plugin.enterprise.manifest_json
}
