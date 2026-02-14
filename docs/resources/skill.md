---
page_title: "agentctx_skill Resource"
subcategory: ""
description: |-
  Manages a skill bundle deployed to one or more cloud object storage targets, with optional Anthropic registry integration.
---

# agentctx_skill (Resource)

Manages a skill bundle deployed to one or more cloud object storage targets.

A skill is a directory of files (the "bundle") that is hashed, uploaded to one or more storage targets, and tracked with an atomic ACTIVE pointer and a JSON manifest. The resource supports the full lifecycle: create, read (refresh with drift detection), update (re-deploy on content change), and destroy (cleanup with configurable retention).

When the optional `anthropic` block is enabled, the resource also registers the skill with the Anthropic Skills API and optionally creates versions automatically when the bundle content changes.

## Example Usage

### Basic Deployment

```hcl
resource "agentctx_skill" "my_skill" {
  source_dir = "./skills/my-skill"
}
```

### With Explicit Targets and Exclusions

```hcl
resource "agentctx_skill" "example" {
  source_dir = "./skills/my-skill"

  targets = ["shared_s3"]

  exclude = [
    "*.log",
    "test/",
    "__pycache__/",
  ]

  prune_deployments  = true
  retain_deployments = 5

  tags = {
    team        = "data-engineering"
    environment = "production"
  }
}
```

### With Anthropic Registry Integration

```hcl
resource "agentctx_skill" "ner_skill" {
  source_dir = "./skills/ner"

  anthropic {
    enabled       = true
    register      = true
    display_title = "Named Entity Recognition"
    auto_version  = true
  }
}
```

### With Pinned Version Strategy

```hcl
resource "agentctx_skill_version" "ner_v3" {
  skill_id   = agentctx_skill.ner_skill.registry_state.skill_id
  source_dir = "./skills/ner-v3"
}

resource "agentctx_skill" "ner_skill" {
  source_dir = "./skills/ner"

  anthropic {
    enabled          = true
    version_strategy = "pinned"
    pinned_version   = agentctx_skill_version.ner_v3.version
  }
}
```

### Validate Only (Dry Run)

```hcl
resource "agentctx_skill" "dry_run" {
  source_dir    = "./skills/experimental"
  validate_only = true
}
```

### Multi-Target Deployment

```hcl
resource "agentctx_skill" "replicated" {
  source_dir = "./skills/replicated-skill"
  targets    = ["us_east_s3", "eu_west_gcs", "backup_azure"]
}
```

## Argument Reference

### Required

- `source_dir` (String) -- Path to the local directory containing the skill source files. The directory is scanned recursively, and all files (excluding those matched by `exclude` patterns and built-in security rules) are included in the bundle.

### Optional

- `targets` (List of String) -- List of target names to deploy to. When omitted, the provider's `default_targets` are used; if those are also empty, every configured target is used (only when exactly one target is defined). Defaults to `[]`.
- `exclude` (List of String) -- Additional gitignore-style glob patterns that exclude files from the bundle. These are applied on top of built-in security excludes (e.g., `.env`, `*.pem`, `credentials.json`). Defaults to `[]`.
- `prune_deployments` (Boolean) -- Whether to prune old deployments after a successful deploy. Defaults to `true`.
- `retain_deployments` (Number) -- Number of old deployments to retain when pruning. Only applies when `prune_deployments` is `true`. Defaults to `5`.
- `allow_external_symlinks` (Boolean) -- Whether to allow symlinks that resolve outside `source_dir`. When `false`, symlinks pointing outside the source directory cause a validation error. Defaults to `false`.
- `validate_only` (Boolean) -- When `true`, the resource validates the bundle (scanning, hashing, exclusion) but does not deploy to any target. Useful for dry runs and CI validation. The resource ID will be prefixed with `validate:`. Defaults to `false`.
- `force_destroy` (Boolean) -- Allow destruction of deployments even if the ACTIVE pointer was modified outside Terraform (e.g., by another process or manual intervention). Defaults to `false`.
- `force_destroy_shared_prefix` (Boolean) -- Allow destruction when the storage prefix is shared with other resources. Defaults to `false`.
- `deep_drift_check` (Boolean) -- When `true`, the Read (refresh) operation performs per-file hash checks rather than relying solely on the bundle hash. This is more thorough but slower. Defaults to `false`.
- `tags` (Map of String) -- Arbitrary key-value tags stored in the deployment manifest. Tags are for organizational purposes and do not affect deployment behavior.

### Blocks

#### `anthropic`

Optional. At most one `anthropic` block may be specified. Configures Anthropic registry integration for this skill.

~> The provider must have an `anthropic` block configured (with a valid `api_key`) for the resource-level `anthropic` block to function. If the resource enables Anthropic integration but the provider does not have an `anthropic` block, Terraform will return an error during apply.

- `enabled` (Boolean) -- Whether Anthropic registry integration is enabled. Defaults to `false`.
- `register` (Boolean) -- Whether to register the skill with the Anthropic registry on create/update. Defaults to `true`.
- `display_title` (String) -- Human-readable display title for the skill in the Anthropic registry. When omitted, the skill name (base name of `source_dir`) is used.
- `auto_version` (Boolean) -- Whether to automatically create a new version in the Anthropic registry when the bundle content changes. Defaults to `true`.
- `version_strategy` (String) -- Version strategy. Must be `"auto"`, `"pinned"`, or `"manual"`. Defaults to `"auto"`.
  - `"auto"` -- versions are created automatically when the bundle changes (requires `auto_version = true`). `pinned_version` must **not** be set.
  - `"pinned"` -- deploy a specific version. `pinned_version` is **required**.
  - `"manual"` -- versions are managed externally (e.g., via `agentctx_skill_version`). `pinned_version` is **required**.
- `pinned_version` (String) -- Version string to use when `version_strategy` is `"pinned"` or `"manual"`. Typically references an `agentctx_skill_version` resource.

## Attribute Reference

In addition to all arguments above, the following attributes are exported:

- `id` (String) -- Unique identifier for the resource instance. Format: `{skill_name}:{deployment_id}` for deployed skills, or `validate:{skill_name}` for validate-only resources.
- `skill_name` (String) -- Derived skill name (base name of `source_dir`).
- `source_hash` (String) -- SHA-256 hash of the source directory structure and metadata. Computed during plan and apply.
- `bundle_hash` (String) -- Deterministic SHA-256 hash over all file contents in the bundle. Format: `sha256:{hex}`.
- `registry_state` (Object) -- State of the skill in the Anthropic registry. Only populated when the `anthropic` block is configured and enabled. Contains:
  - `skill_id` (String) -- Anthropic skill identifier (e.g., `skill_01AbCdEf...`).
  - `deployed_version` (String) -- Currently deployed version string (e.g., `v1`).
  - `latest_version` (String) -- Latest available version string.
- `target_states` (Map of Object) -- Per-target deployment state. Keys are target names. Each entry contains:
  - `active_deployment_id` (String) -- Deployment ID currently pointed to by the ACTIVE marker.
  - `staged_deployment_id` (String) -- Deployment ID staged but not yet promoted to active.
  - `deployed_bundle_hash` (String) -- Bundle hash of the active deployment.
  - `last_synced_at` (String) -- RFC 3339 timestamp of the last successful sync.
  - `managed_deploy_ids` (List of String) -- List of deployment IDs managed by this resource instance.

## Import

The `agentctx_skill` resource supports importing existing deployments. The import ID uses a compound format with comma-separated segments.

### Import Formats

**Skill ID only** (Anthropic registry):

```shell
terraform import agentctx_skill.example skill_01AbCdEf12345678
```

**Target deployment only:**

```shell
terraform import agentctx_skill.example target:shared_s3:dep_20260213T200102Z_6f2c9a1b
```

**Combined** (Anthropic skill + target deployment):

```shell
terraform import agentctx_skill.example "skill_01AbCdEf12345678,target:shared_s3:dep_20260213T200102Z_6f2c9a1b"
```

**Multiple targets:**

```shell
terraform import agentctx_skill.example "target:us_east:dep_20260213T200102Z_6f2c9a1b,target:eu_west:dep_20260213T200102Z_a1b2c3d4"
```

-> After import, you must add the `source_dir` argument to your configuration and run `terraform plan` to reconcile the imported state with your local source files.

## Lifecycle Behavior

### Create

1. Scans the source directory and computes a deterministic bundle hash.
2. If `validate_only = true`, saves minimal state and returns without deploying.
3. If Anthropic integration is enabled, creates the skill in the registry (and optionally a version).
4. Deploys the bundle to each resolved target with an atomic ACTIVE pointer swap.
5. Prunes old deployments if `prune_deployments` is enabled.

### Read (Refresh)

1. For each target, reads the ACTIVE pointer and manifest.
2. Compares the deployed bundle hash with the expected hash in state.
3. If `deep_drift_check` is enabled, verifies individual file hashes.
4. If the manifest is missing (deleted externally), removes the resource from state.

### Update

1. Re-scans the source directory and computes the new bundle hash.
2. If the bundle hash changed and Anthropic `auto_version` is enabled, creates a new version.
3. Re-deploys to each target with a new deployment ID.
4. Prunes old deployments if enabled.

### Destroy

1. Removes all managed deployments from each target.
2. If Anthropic `destroy_remote` is enabled on the provider:
   - Deletes all managed versions from the registry.
   - If no other versions remain, deletes the skill itself.
   - If versions created by other processes remain, logs a warning and preserves the skill.

## Built-in File Exclusions

The following files are **always** excluded from bundles and cannot be overridden:

### Security Excludes

- `.git/`, `.aws/`, `.ssh/` -- Sensitive directories
- `.env`, `.env.*` -- Environment variable files (`.env.example` and `.env.template` are allowed)
- `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks` -- Private keys and certificate stores
- `id_rsa`, `id_ed25519` -- SSH private keys (anywhere in the tree)

### Convenience Excludes

- `node_modules/` -- Node.js dependencies
- `.venv/` -- Python virtual environments
- `__pycache__/` -- Python bytecode cache
- `.DS_Store`, `Thumbs.db` -- OS metadata files
- `.terraform/` -- Terraform working directory
- `*.tfstate*` -- Terraform state files
