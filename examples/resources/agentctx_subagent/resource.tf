# Basic sub-agent: a read-only code reviewer
resource "agentctx_subagent" "code_reviewer" {
  name        = "code-reviewer"
  description = "Expert code review specialist. Proactively reviews code for quality, security, and maintainability."
  output_dir  = "${path.module}/.claude/agents"
  model       = "sonnet"
  tools       = ["Read", "Grep", "Glob", "Bash"]

  prompt = <<-EOT
    You are a senior code reviewer ensuring high standards of code quality.

    When invoked:
    1. Run git diff to see recent changes
    2. Focus on modified files
    3. Begin review immediately

    Review checklist:
    - Code is clear and readable
    - No exposed secrets or API keys
    - Proper error handling
    - Good test coverage

    Provide feedback organized by priority:
    - Critical issues (must fix)
    - Warnings (should fix)
    - Suggestions (consider improving)
  EOT
}

# Full-featured sub-agent with hooks, MCP servers, and memory
resource "agentctx_subagent" "db_reader" {
  name             = "db-reader"
  description      = "Execute read-only database queries. Use when analyzing data or generating reports."
  output_dir       = "${path.module}/.claude/agents"
  tools            = ["Bash"]
  disallowed_tools = ["Write", "Edit"]
  permission_mode  = "dontAsk"
  max_turns        = 25
  memory           = "project"

  prompt = <<-EOT
    You are a database analyst with read-only access.
    Execute SELECT queries to answer questions about the data.
  EOT

  mcp_server {
    name = "postgres"
  }

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

# Coordinator sub-agent that delegates to specific sub-agents
resource "agentctx_subagent" "coordinator" {
  name        = "coordinator"
  description = "Coordinates work across specialized agents"
  output_dir  = "${path.module}/.claude/agents"
  tools       = ["Task(worker, researcher)", "Read", "Bash"]
  skills      = ["api-conventions", "error-handling-patterns"]

  prompt = <<-EOT
    You are a project coordinator. Delegate tasks to the worker
    and researcher sub-agents as appropriate.
  EOT
}

output "code_reviewer_file" {
  value = agentctx_subagent.code_reviewer.file_path
}

output "code_reviewer_hash" {
  value = agentctx_subagent.code_reviewer.content_hash
}
