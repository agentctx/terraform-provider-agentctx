resource "agentctx_skill_version" "v3" {
  skill_id   = agentctx_skill.example.registry_state.skill_id
  source_dir = "./skills/my-skill-v3"
}

output "version" {
  value = agentctx_skill_version.v3.version
}

output "bundle_hash" {
  value = agentctx_skill_version.v3.bundle_hash
}
