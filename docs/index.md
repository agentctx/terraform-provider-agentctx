---
page_title: "agentctx Provider"
subcategory: ""
description: |-
  The agentctx provider manages Claude Code context artifacts: skills (with optional Anthropic registry integration), sub-agents, and plugins.
---

# agentctx Provider

The agentctx provider manages Claude Code context artifacts:

- **Skills** deployed to cloud storage targets (**Amazon S3**, **Azure Blob Storage**, **Google Cloud Storage**) with optional **Anthropic Skills API** integration.
- **Sub-agents** rendered as local Markdown files following Claude Code sub-agent format.
- **Plugins** rendered as local plugin directory structures (`plugin.json`, hooks, agents, skills, MCP/LSP config, and bundled files).

Key capabilities:

- **Multi-target deployment** -- deploy the same skill bundle to multiple storage backends simultaneously.
- **Deterministic hashing** -- SHA-256 content hashing ensures consistent, reproducible deployments.
- **Atomic deploys** -- conditional writes protect against concurrent modifications.
- **Drift detection** -- refresh operations detect and surface out-of-band changes.
- **Deployment pruning** -- automatic cleanup of old deployments with configurable retention.
- **Anthropic registry** -- optional skill registration and versioning through the Anthropic Skills API.
- **Sub-agent generation** -- produce local Claude Code sub-agent definitions with hooks and MCP configuration.
- **Plugin generation** -- produce local Claude Code plugin bundles with manifest, hooks, MCP/LSP, and packaged artifacts.

## Resource Docs

- [agentctx_skill](./resources/skill.md)
- [agentctx_skill_version](./resources/skill_version.md)
- [agentctx_subagent](./resources/subagent.md)
- [agentctx_plugin](./resources/plugin.md)

## Example Usage

### Minimal Configuration (Single S3 Target)

```hcl
provider "agentctx" {
  target {
    name   = "primary"
    type   = "s3"
    bucket = "my-skill-artifacts"
    region = "us-east-1"
  }
}
```

### Multi-Target with Anthropic Integration

```hcl
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

  target {
    name            = "backup_gcs"
    type            = "gcs"
    bucket          = "my-ai-backup"
    prefix          = "skills/"
    max_retries     = 5
    timeout_seconds = 60
  }
}

variable "anthropic_api_key" {
  type      = string
  sensitive = true
}
```

### Azure Target

```hcl
provider "agentctx" {
  target {
    name             = "azure_primary"
    type             = "azure"
    storage_account  = "myskillstorage"
    container_name   = "skills"
    encryption_scope = "my-scope"
    prefix           = "v1/"
  }
}
```

## Authentication

The provider delegates authentication to the underlying cloud SDKs:

| Target Type | Authentication Method |
|-------------|----------------------|
| **S3** | AWS SDK default credential chain (environment variables, shared credentials file, IAM role, etc.) |
| **Azure** | Azure `DefaultAzureCredential` (environment variables, managed identity, Azure CLI, etc.) |
| **GCS** | Google Application Default Credentials (environment variables, service account key, workload identity, etc.) |
| **Anthropic** | API key provided via the `api_key` attribute in the `anthropic` block. |

## Schema

### Optional

- `canonical_store` (String) -- Name of the canonical store used for source-of-truth reads. Defaults to `"source"` when omitted.
- `max_concurrency` (Number) -- Maximum number of concurrent operations the provider will perform across all targets. Defaults to `16`.
- `default_targets` (List of String) -- List of target names that resources will replicate to when their own `targets` argument is not set.

### Blocks

#### `anthropic`

Optional. At most one `anthropic` block may be specified. Configures the Anthropic API client for remote skill operations.

**Required:**

- `api_key` (String, Sensitive) -- Anthropic API key used for authentication. This value is sensitive and will not appear in plan output.

**Optional:**

- `base_url` (String) -- Override the Anthropic API base URL. Useful for testing with a mock server.
- `max_retries` (Number) -- Maximum number of retries for failed Anthropic API requests. Defaults to `3`.
- `destroy_remote` (Boolean) -- Whether to destroy the remote Anthropic resource when the Terraform resource is destroyed. Defaults to `false`.
- `timeout_seconds` (Number) -- Timeout in seconds for individual Anthropic API requests. Defaults to `60`.

#### `target`

Required. At least one `target` block must be configured. Defines a storage target for skill artifacts.

**Required:**

- `name` (String) -- Unique name used to reference this target in resource configurations and `default_targets`.
- `type` (String) -- Storage backend type. Must be `"s3"`, `"azure"`, or `"gcs"`.

**Optional (all target types):**

- `prefix` (String) -- Key prefix prepended to all object paths within the target bucket or container.
- `max_concurrency` (Number) -- Maximum number of concurrent operations for this specific target. Overrides the provider-level `max_concurrency`.
- `max_retries` (Number) -- Maximum number of retries for failed operations against this target. Defaults to `3`.
- `timeout_seconds` (Number) -- Timeout in seconds for individual operations against this target. Defaults to `30`.
- `retry_backoff` (String) -- Retry backoff strategy. Must be `"exponential"` or `"linear"`. Defaults to `"exponential"`.

**S3-specific:**

- `bucket` (String) -- S3 bucket name. Required for `s3` targets.
- `region` (String) -- AWS region for the S3 bucket. Required for `s3` targets.
- `kms_key_id` (String) -- AWS KMS key ID or ARN used for server-side encryption of S3 objects.

**Azure-specific:**

- `storage_account` (String) -- Azure Storage account name. Required for `azure` targets.
- `container_name` (String) -- Azure Blob Storage container name. Required for `azure` targets.
- `encryption_scope` (String) -- Azure encryption scope to apply when writing blobs.

**GCS-specific:**

- `bucket` (String) -- GCS bucket name. Required for `gcs` targets.
- `kms_key_name` (String) -- GCS Cloud KMS key resource name used for object encryption.

## Target Resolution

When a resource does not explicitly set the `targets` attribute, the provider resolves the effective target list using the following precedence:

1. **Explicit `targets`** on the resource -- if set, use exactly these targets.
2. **`default_targets`** on the provider -- if the resource omits `targets` and the provider has `default_targets`, use those.
3. **Implicit single target** -- if the provider defines exactly one target and neither the resource nor the provider specifies `default_targets`, that single target is used automatically.

~> If the provider has two or more targets and neither `default_targets` on the provider nor `targets` on the resource is set, Terraform will return an error during planning. Either set `default_targets` on the provider or specify `targets` on each resource.
