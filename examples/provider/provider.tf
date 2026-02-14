terraform {
  required_providers {
    agentctx = {
      source = "agentctx/agentctx"
    }
  }
}

provider "agentctx" {
  canonical_store = "source"
  max_concurrency = 16

  default_targets = ["shared_s3"]

  anthropic {
    api_key         = var.anthropic_api_key
    max_retries     = 3
    destroy_remote  = false
    timeout_seconds = 60
  }

  target {
    name   = "shared_s3"
    type   = "s3"
    bucket = "my-ai-platform"
    region = "us-east-1"
    prefix = "agent-context/skills/"
  }
}

variable "anthropic_api_key" {
  type      = string
  sensitive = true
}
