# Terraform Provider agentctx

Deploy AI skill bundles to cloud storage with versioning, drift detection, and optional [Anthropic](https://www.anthropic.com/) registry integration.

> [Registry](https://registry.terraform.io/providers/agentctx/agentctx/latest) | [Documentation](https://registry.terraform.io/providers/agentctx/agentctx/latest/docs)

## Quick Start

```hcl
terraform {
  required_providers {
    agentctx = {
      source  = "agentctx/agentctx"
      version = "~> 0.1"
    }
  }
}

# Configure a storage target and Anthropic integration
provider "agentctx" {
  anthropic {
    api_key = var.anthropic_api_key
  }

  target {
    name   = "production"
    type   = "s3"
    bucket = "my-ai-skills"
    region = "us-east-1"
    prefix = "skills/"
  }
}

# Deploy a skill from a local directory
resource "agentctx_skill" "sec_filings" {
  source_dir = "./skills/analyzing-sec-filings"

  anthropic {
    enabled       = true
    register      = true
    display_title = "Analyzing SEC Filings"
    auto_version  = true
  }
}
```

```sh
terraform init
terraform apply
```

This will:
1. Bundle `./skills/analyzing-sec-filings` and deploy it to S3
2. Register the skill in Anthropic's registry as **"Analyzing SEC Filings"**
3. Create a new version automatically whenever the skill content changes

## Examples

### Multi-cloud replication

```hcl
provider "agentctx" {
  default_targets = ["aws", "gcs"]

  target {
    name   = "aws"
    type   = "s3"
    bucket = "skills-us-east"
    region = "us-east-1"
  }

  target {
    name   = "gcs"
    type   = "gcs"
    bucket = "skills-eu-west"
  }

  target {
    name             = "azure"
    type             = "azure"
    storage_account  = "myskillstore"
    container_name   = "skills"
  }
}

# Deploys to aws + gcs (default_targets)
resource "agentctx_skill" "sec_filings" {
  source_dir = "./skills/analyzing-sec-filings"
}

# Deploys to all three targets
resource "agentctx_skill" "classifier" {
  source_dir = "./skills/classifier"
  targets    = ["aws", "gcs", "azure"]
}
```

### Pinned version promotion

```hcl
resource "agentctx_skill_version" "v3" {
  skill_id   = agentctx_skill.sec_filings.registry_state.skill_id
  source_dir = "./skills/sec-filings-v3"
}

resource "agentctx_skill" "sec_filings" {
  source_dir = "./skills/analyzing-sec-filings"

  anthropic {
    enabled          = true
    version_strategy = "pinned"
    pinned_version   = agentctx_skill_version.v3.version
  }
}
```

## Supported Targets

| Target | Auth | Config |
|--------|------|--------|
| **Amazon S3** | AWS default credential chain | `bucket`, `region`, `kms_key_id` |
| **Google Cloud Storage** | Application Default Credentials | `bucket`, `kms_key_name` |
| **Azure Blob Storage** | `DefaultAzureCredential` | `storage_account`, `container_name`, `encryption_scope` |

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0

## Building from Source

```sh
git clone https://github.com/agentctx/terraform-provider-agentctx.git
cd terraform-provider-agentctx
make install
```

## Development

```sh
make test          # unit tests
make testacc       # acceptance tests (requires cloud credentials)
make lint          # vet + fmt
```
