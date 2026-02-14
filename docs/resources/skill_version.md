---
page_title: "agentctx_skill_version Resource"
subcategory: ""
description: |-
  Creates an immutable skill version in the Anthropic registry from a local source directory.
---

# agentctx_skill_version (Resource)

Creates an immutable skill version in the Anthropic registry from a local source directory. Each version is a snapshot of the skill's source files at a point in time.

This resource is **immutable** -- changes to `skill_id` or `source_dir` force the resource to be destroyed and recreated. In-place updates are not supported.

~> This resource requires the provider to have an `anthropic` block configured with a valid API key. If the `anthropic` block is missing, Terraform will return an error during apply.

## Example Usage

### Basic Version

```hcl
resource "agentctx_skill_version" "v1" {
  skill_id   = agentctx_skill.my_skill.registry_state.skill_id
  source_dir = "./skills/my-skill"
}
```

### Pinned Version Workflow

Use `agentctx_skill_version` to create explicit versions, then pin a skill to a specific version:

```hcl
resource "agentctx_skill" "ner" {
  source_dir = "./skills/ner"

  anthropic {
    enabled          = true
    auto_version     = false
    version_strategy = "pinned"
    pinned_version   = agentctx_skill_version.ner_v2.version
  }
}

resource "agentctx_skill_version" "ner_v2" {
  skill_id   = agentctx_skill.ner.registry_state.skill_id
  source_dir = "./skills/ner-v2"
}
```

### Multiple Versions

```hcl
resource "agentctx_skill_version" "canary" {
  skill_id   = agentctx_skill.my_skill.registry_state.skill_id
  source_dir = "./skills/my-skill-canary"
}

resource "agentctx_skill_version" "stable" {
  skill_id   = agentctx_skill.my_skill.registry_state.skill_id
  source_dir = "./skills/my-skill-stable"
}
```

## Argument Reference

### Required

- `skill_id` (String) -- Anthropic skill ID to create the version for. Typically references `agentctx_skill.<name>.registry_state.skill_id`. Changing this forces a new resource to be created.
- `source_dir` (String) -- Path to the local directory containing the skill source files. The directory is scanned, hashed, and uploaded to the Anthropic registry as a multipart form. Changing this forces a new resource to be created.

## Attribute Reference

In addition to all arguments above, the following attributes are exported:

- `id` (String) -- Unique identifier for the skill version resource. This is the version ID assigned by the Anthropic API.
- `version` (String) -- Version string assigned by the Anthropic registry (e.g., `v1`, `v2`).
- `bundle_hash` (String) -- Deterministic SHA-256 hash of the bundle uploaded with this version. Format: `sha256:{hex}`.
- `created_at` (String) -- RFC 3339 timestamp when the version was created in the Anthropic registry.

## Lifecycle Behavior

### Create

1. Scans the source directory and computes a bundle hash.
2. Uploads all source files to the Anthropic registry as a multipart form.
3. Saves the version ID, version string, bundle hash, and creation timestamp to state.

### Read (Refresh)

1. Fetches version metadata from the Anthropic API.
2. If the version no longer exists (HTTP 404), removes the resource from state so Terraform plans recreation.
3. Updates `version` and `created_at` from the API; preserves `bundle_hash` from state (not returned by the API).

### Update

Not supported. Changes to either `skill_id` or `source_dir` force resource replacement (destroy + create).

### Destroy

- If the provider's `anthropic` block has `destroy_remote = true`, the version is deleted from the Anthropic registry.
- If `destroy_remote = false` (the default), the remote version is preserved and only the Terraform state is removed.
- If the version was already deleted externally (HTTP 404), the error is suppressed.

## Destroy Behavior

-> By default, destroying an `agentctx_skill_version` resource only removes it from Terraform state. The version remains in the Anthropic registry. Set `destroy_remote = true` on the provider's `anthropic` block to also delete the remote version.
