# Terraform Provider for AgentCtx

- Terraform Registry: https://registry.terraform.io/providers/agentctx/agentctx/latest
- Documentation: https://registry.terraform.io/providers/agentctx/agentctx/latest/docs

## Overview

The AgentCtx provider manages agent context skills and their versions across cloud storage targets, with optional Anthropic registry integration. It supports deploying skill bundles to **Amazon S3**, **Azure Blob Storage**, and **Google Cloud Storage**.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.24 (to build the provider plugin)

## Usage

```hcl
terraform {
  required_providers {
    agentctx = {
      source  = "agentctx/agentctx"
      version = "~> 0.1"
    }
  }
}

provider "agentctx" {
  target {
    name   = "primary"
    type   = "s3"
    bucket = "my-skill-artifacts"
    region = "us-east-1"
  }
}

resource "agentctx_skill" "example" {
  source_dir = "./skills/my-skill"
  targets    = ["primary"]
}
```

## Features

- **Multi-target deployment** -- deploy the same skill bundle to S3, Azure Blob Storage, and GCS simultaneously
- **Deterministic hashing** -- SHA-256 content hashing ensures consistent, reproducible deployments
- **Atomic deploys** -- conditional writes protect against concurrent modifications
- **Drift detection** -- refresh operations detect and surface out-of-band changes
- **Deployment pruning** -- automatic cleanup of old deployments with configurable retention
- **Anthropic registry** -- optional skill registration and versioning through the Anthropic Skills API

## Building the Provider

Clone the repository:

```sh
git clone https://github.com/agentctx/terraform-provider-agentctx.git
cd terraform-provider-agentctx
```

Build and install locally:

```sh
make install
```

## Development

### Running Tests

```sh
make test
```

### Running Acceptance Tests

Acceptance tests require valid cloud credentials for the configured targets.

```sh
make testacc
```

### Linting

```sh
make lint
```

## Documentation

Full documentation is available on the [Terraform Registry](https://registry.terraform.io/providers/agentctx/agentctx/latest/docs).

## License

See [LICENSE](LICENSE) for details.
