resource "agentctx_skill_version" "ner_v3" {
  skill_id   = agentctx_skill.pipeline_ner.registry_state.skill_id
  source_dir = "./skills/pipeline-ner-v3"
}

output "version" {
  value = agentctx_skill_version.ner_v3.version
}

output "bundle_hash" {
  value = agentctx_skill_version.ner_v3.bundle_hash
}
