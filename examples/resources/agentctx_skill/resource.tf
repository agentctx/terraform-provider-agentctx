resource "agentctx_skill" "pipeline_ner" {
  source_dir = "./skills/pipeline-ner"

  targets = ["shared_s3"]

  exclude = ["*.log", "test/"]

  prune_deployments  = true
  retain_deployments = 5

  anthropic {
    enabled       = true
    register      = true
    display_title = "Biopharma Pipeline NER"
    auto_version  = true
  }

  tags = {
    team = "data-engineering"
  }
}

output "skill_name" {
  value = agentctx_skill.pipeline_ner.skill_name
}

output "source_hash" {
  value = agentctx_skill.pipeline_ner.source_hash
}
