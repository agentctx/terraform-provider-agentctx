resource "agentctx_skill" "example" {
  source_dir = "./skills/my-skill"

  targets = ["shared_s3"]

  exclude = ["*.log", "test/"]

  prune_deployments  = true
  retain_deployments = 5

  anthropic {
    enabled       = true
    register      = true
    display_title = "My Example Skill"
    auto_version  = true
  }

  tags = {
    team = "data-engineering"
  }
}

output "skill_name" {
  value = agentctx_skill.example.skill_name
}

output "source_hash" {
  value = agentctx_skill.example.source_hash
}
