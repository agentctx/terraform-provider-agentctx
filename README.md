# AgentCtx Terraform Provider — Product Specification v2

## Revision Notes

This revision addresses the following gaps and issues from v1:

1. **State management & recovery** — Repair logic now specifies how to verify deployment completeness without a manifest, uses file listing with prefix scan, and tracks staged deployment IDs in state.
2. **Concurrent apply / state locking** — Added conditional write semantics for ACTIVE pointer per cloud provider, and documented concurrency constraints.
3. **Partial upload cleanup** — Incomplete staging prefixes are now explicitly tracked and cleaned up on next apply.
4. **Hash computation algorithm** — Pinned down a deterministic, cross-platform hash algorithm (sorted paths + content, no OS-dependent enumeration).
5. **Download integrity verification** — Added per-file hash verification after downloading Anthropic bundles before deploying to targets.
6. **Anthropic version creation semantics** — Resolved the dangling "see below" reference; version creation on source change is now opt-in via `auto_version`.
7. **Manual version strategy** — Committed to using `pinned_version` referencing `agentctx_skill_version` resources; removed ambiguity.
8. **Anthropic "skill if empty" deletion** — Defined empty check semantics with race-safety via compare-and-delete.
9. **Target defaulting precedence** — Explicit precedence order defined; empty list `[]` is now an error, not silent fallback.
10. **Drift detection scope** — Documented that per-file tampering within a deployment is a non-goal; added optional deep drift mode.
11. **Eventual consistency** — Added read-after-write guidance per cloud provider.
12. **`force_destroy_shared_prefix`** — Fully defined blocking vs fallback behavior.
13. **Retention vs pruning lifecycle** — Clarified that pruning happens on every apply, not just destroy.
14. **Missing encryption fields** — Added encryption config for Azure and GCS targets.
15. **Content-type handling** — Defined content-type inference from file extension.
16. **Per-target timeout/retry** — Added retry and timeout configuration per target.
17. **Plan output format** — Defined what users see during `terraform plan`.
18. **Import story for existing deployments** — Added import path for pre-existing target deployments.
19. **Minor issues** — Fixed provider version format, defined deployment_id generation algorithm, documented `validate_only`.

---

## 1) Scope

This provider manages skill bundles authored locally and deployed to cloud object storage, with optional canonical versioning via Anthropic Skills API.

**Supported:**

- Skill authoring in repo (`source_dir`)
- Deployment targets: S3, Azure Blob, GCS
- Optional Anthropic Skills API: create skills, create versions, fetch a specific version bundle (for promotion/pinned deploys)
- Drift detection and safe deletion using a manifest + commit pointer

**Not supported:**

- Local filesystem target
- Other model providers
- "Open standard only" mode without Anthropic (you can deploy without Anthropic enabled, but the provider is designed around these specific targets)
- `agents`/`settings` resources (can be added later using the same bundle mechanism)

---

## 2) Core Concepts

### 2.1 Bundle

A bundle is a directory (skill) packaged and deployed as a unit.

Bundle fields:

- `bundle_name` — skill name
- `source_hash` — deterministic hash of deployable files (post-exclude); see §2.4
- `bundle_hash` — hash of the deployed bundle (equals `source_hash` if deploying from local; may differ if deploying from Anthropic pinned version)
- `deployment_id` — unique id for each commit per target; see §2.5

### 2.2 Canonical Store

Provider-level setting:

- `canonical_store = "source"` (default): local repo is canonical. Targets receive bytes from `source_dir`. Anthropic integration (if enabled) is a registry/mirror.
- `canonical_store = "anthropic"`: Anthropic is canonical. Targets receive bytes by downloading an Anthropic version bundle.

This is explicit and eliminates split-brain semantics.

### 2.3 Commit Model for Object Stores

Object stores are not atomic for multi-file updates. Therefore deployments are commit-based:

1. Upload to a staging deployment prefix
2. Upload manifest for that deployment
3. Atomically "activate" by writing a single small pointer object: `ACTIVE`

The active deployment is defined solely by `ACTIVE → deployment_id`.

### 2.4 Hash Computation Algorithm

All hashes use SHA-256. Bundle/source hashes are computed deterministically as follows:

1. Enumerate all deployable files (post-exclude) and compute their relative paths from `source_dir`.
2. Sort relative paths lexicographically using byte-order comparison (not locale-dependent).
3. For each file in sorted order, compute: `SHA256(file_contents)` using raw bytes (no line-ending normalization).
4. Concatenate an entry string per file: `<relative_path>\0<hex_sha256>\n`
5. Compute the final hash: `SHA256(concatenated_entry_strings)`

This ensures identical results across operating systems regardless of filesystem enumeration order. The `\0` separator prevents collisions between paths and hashes.

Implementation note: the intermediate per-file hashes are also stored in the manifest's `files` map and used for individual file integrity checks.

### 2.5 Deployment ID Generation

Format: `dep_<timestamp>_<random>`

- `<timestamp>` = UTC time formatted as `YYYYMMDD'T'HHmmss'Z'` (e.g., `20260213T200102Z`)
- `<random>` = 8 hex characters from a cryptographically secure random source

Example: `dep_20260213T200102Z_6f2c9a1b`

The random suffix guarantees uniqueness under concurrent execution. The timestamp prefix enables chronological sorting for retention/pruning.

---

## 3) Provider Configuration

### 3.1 Provider schema

```hcl
provider "agentctx" {
  canonical_store = "source"      # "source" | "anthropic"
  max_concurrency = 16            # global across all resources/targets

  # Target resolution precedence:
  # 1. Explicit resource `targets` attribute
  # 2. Provider `default_targets`
  # 3. Implicit single target (only if exactly 1 target is configured)
  # An empty list `targets = []` on a resource is a validation error.
  default_targets = ["shared_s3"]

  anthropic {
    api_key         = var.anthropic_api_key
    max_retries     = 3
    destroy_remote  = false       # default: do NOT delete Anthropic skills/versions on destroy
    timeout_seconds = 60
  }

  target "shared_s3" {
    type   = "s3"
    bucket = "deerfield-ai-platform"
    region = "us-east-1"
    prefix = "agent-context/skills/"

    # Optional
    kms_key_id      = "arn:aws:kms:..."
    max_concurrency = 8
    max_retries     = 3           # default: 3
    timeout_seconds = 30          # per-operation timeout; default: 30
    retry_backoff   = "exponential" # "exponential" | "linear"; default: exponential
  }

  target "shared_azure" {
    type            = "azure"
    storage_account = "myaccount"
    container_name  = "skills"
    prefix          = "agent-context/skills/"

    # Optional
    encryption_scope = "my-scope"  # Azure encryption scope name
    max_concurrency  = 8
    max_retries      = 3
    timeout_seconds  = 30
    retry_backoff    = "exponential"
  }

  target "shared_gcs" {
    type   = "gcs"
    bucket = "my-gcs-bucket"
    prefix = "agent-context/skills/"

    # Optional
    kms_key_name    = "projects/.../cryptoKeys/my-key"  # Cloud KMS key
    max_concurrency = 8
    max_retries     = 3
    timeout_seconds = 30
    retry_backoff   = "exponential"
  }
}
```

### 3.2 Target Resolution Precedence

Targets for a resource are resolved in this order:

1. **Explicit resource `targets`** — if set on the resource, use exactly these targets.
2. **Provider `default_targets`** — if the resource omits `targets` and the provider sets `default_targets`, use those.
3. **Implicit single target** — if the provider defines exactly 1 target, and neither the resource nor provider specifies `default_targets`, use that single target implicitly.

Validation rules:

- 0 targets configured on provider → configuration error (nowhere to deploy).
- Resource sets `targets = []` (empty list) → validation error. Omit the attribute entirely to use defaults; don't pass empty.
- 2+ targets on provider, no `default_targets`, resource omits `targets` → validation error with a message suggesting either setting `default_targets` or explicit resource `targets`.

---

## 4) Target Types

### 4.1 S3 target

```hcl
target "shared_s3" {
  type   = "s3"
  bucket = "..."
  region = "us-east-1"
  prefix = "agent-context/skills/"

  # Optional
  kms_key_id      = "arn:aws:kms:..."
  max_concurrency = 8
  max_retries     = 3
  timeout_seconds = 30
  retry_backoff   = "exponential"
}
```

Auth: AWS SDK default chain (env/profile/role).

Conditional write: S3 supports `If-None-Match` for `PutObject` (used for ACTIVE pointer safety; see §7.3).

Content-type: inferred from file extension (see §4.4).

### 4.2 Azure Blob target

```hcl
target "shared_azure" {
  type            = "azure"
  storage_account = "..."
  container_name  = "..."
  prefix          = "agent-context/skills/"

  # Optional
  encryption_scope = "my-scope"
  max_concurrency  = 8
  max_retries      = 3
  timeout_seconds  = 30
  retry_backoff    = "exponential"
}
```

Auth: DefaultAzureCredential.

Conditional write: Azure supports blob leasing for mutual exclusion on ACTIVE (see §7.3).

### 4.3 GCS target

```hcl
target "shared_gcs" {
  type   = "gcs"
  bucket = "..."
  prefix = "agent-context/skills/"

  # Optional
  kms_key_name    = "projects/.../cryptoKeys/my-key"
  max_concurrency = 8
  max_retries     = 3
  timeout_seconds = 30
  retry_backoff   = "exponential"
}
```

Auth: Application Default Credentials.

Conditional write: GCS supports `ifGenerationMatch` for conditional overwrite of ACTIVE (see §7.3).

### 4.4 Content-Type Handling

Files uploaded to object stores are assigned a `Content-Type` based on file extension:

| Extension | Content-Type |
|-----------|-------------|
| `.md` | `text/markdown; charset=utf-8` |
| `.json` | `application/json` |
| `.py` | `text/x-python` |
| `.yaml`, `.yml` | `application/x-yaml` |
| `.txt` | `text/plain; charset=utf-8` |
| `.html` | `text/html` |
| (all others) | `application/octet-stream` |

The `ACTIVE` pointer is always `text/plain; charset=utf-8`. Manifests are always `application/json`.

---

## 5) Object Layout

For each skill `skill_name`, each target stores:

```
<prefix>/<skill_name>/
  .agentctx/
    ACTIVE
    deployments/
      <deployment_id>/
        manifest.json
        files/
          SKILL.md
          scripts/helper.py
          ...
```

Important: There is no "current files" directory at the root. The active deployment is always read via `ACTIVE → manifest → files`.

This keeps "promotions" and "rollbacks" trivial: you can repoint ACTIVE (in a controlled apply) without rewriting large numbers of objects (optional later enhancement).

---

## 6) Manifest

### 6.1 Manifest path

```
<prefix>/<skill_name>/.agentctx/deployments/<deployment_id>/manifest.json
```

### 6.2 Manifest schema (v2)

```json
{
  "schema_version": 2,
  "provider_version": "0.1.0",
  "resource_type": "skill",
  "resource_name": "pipeline-ner",

  "canonical_store": "anthropic",
  "deployment_id": "dep_20260213T200102Z_6f2c9a1b",
  "created_at": "2026-02-13T20:01:02Z",

  "source_hash": "sha256:abc123...",
  "bundle_hash": "sha256:def456...",

  "origin": {
    "type": "source",
    "source_dir": "./skills/pipeline-ner"
  },

  "registry": {
    "type": "anthropic",
    "skill_id": "skill_01AbCdEf...",
    "version": "v3",
    "bundle_hash": "sha256:def456..."
  },

  "files": {
    "SKILL.md": "sha256:aaa...",
    "scripts/helper.py": "sha256:bbb..."
  }
}
```

Notes:

- `provider_version` uses standard semver without suffixes (e.g., `0.1.0`, not `0.1.0-cloud-anthropic`). The provider variant is not encoded here; use `canonical_store` and target types to distinguish configurations.
- `source_hash` always records what the repo contained (post-exclude), computed per §2.4.
- `bundle_hash` records what you actually deployed, also computed per §2.4 over the deployed files.
- When `canonical_store="anthropic"`, `bundle_hash` should match `registry.bundle_hash` (computed from downloaded bundle) and may differ from `source_hash` (if pinned to an older version).
- The `files` map contains per-file SHA-256 hashes used for integrity verification and file-level drift/diff reporting.

---

## 7) Commit Protocol (Required)

A deployment to a target is **committed** if and only if:

- `ACTIVE` exists and points to a `deployment_id`
- The manifest for that deployment exists
- All files referenced in the manifest exist

### 7.1 Apply steps per target

For each target:

1. Compute `deployment_id` per §2.5.
2. Record the `deployment_id` in Terraform state as `staged_deployment_id` for this target (enables cleanup on failure).
3. Upload all files under: `.../.agentctx/deployments/<deployment_id>/files/<relpath>`
4. Upload `manifest.json` under: `.../.agentctx/deployments/<deployment_id>/manifest.json`
5. Write/overwrite `ACTIVE` with the `deployment_id` (using conditional write where supported; see §7.3).
6. Clear `staged_deployment_id` from state; record `deployment_id` as `active_deployment_id`.

`ACTIVE` contents: the `deployment_id` as a plain UTF-8 string (no newline, no JSON wrapping).

### 7.2 Failure behavior & repair

**Failure before ACTIVE is updated:**

The new deployment is staged but not live. The previous active deployment remains active. On next `terraform apply`:

1. Check state for any `staged_deployment_id` that was never activated.
2. Delete all objects under that staged prefix (`.../.agentctx/deployments/<staged_deployment_id>/`).
3. Proceed with a fresh deployment.

**Failure after ACTIVE is updated but deployment is incomplete (rare):**

ACTIVE points to a deployment whose manifest is missing or whose files are incomplete. On next `terraform plan`/`apply`:

1. Read ACTIVE to get the deployment_id.
2. Attempt to read the manifest at the expected path.
3. If manifest exists, verify all files listed in `manifest.files` exist using `HEAD` / metadata checks (not full downloads).
4. If manifest is missing OR files are incomplete:
   a. If the deployment_id matches the last known `active_deployment_id` in Terraform state (i.e., this provider wrote it), attempt repair by re-uploading the missing manifest and/or files from the canonical source.
   b. If the deployment_id is unknown (external actor or state mismatch), roll back ACTIVE to the previous `active_deployment_id` from state, if available.
   c. If no previous deployment_id exists in state, fail with error: `"Target <target> has broken ACTIVE pointer to <deployment_id> with missing manifest. No previous deployment in state to roll back to. Manual intervention required."`
5. Log all repair actions as Terraform diagnostics at WARN level.

### 7.3 Concurrent Apply Protection

Writing the ACTIVE pointer uses conditional writes where the cloud provider supports them, to prevent two concurrent applies from clobbering each other:

- **S3**: Use `PutObject` with `If-None-Match: *` for initial creation. For updates, read the current `ETag`, then use `If-Match: <etag>` on the subsequent `PutObject`. If the condition fails (412), the apply fails with a clear error: `"ACTIVE pointer was modified by another process. Re-run terraform apply."` Note: S3 conditional writes require the bucket to have strong consistency (default since December 2020).
- **GCS**: Use `ifGenerationMatch` on the object write. Read the current `generation`, then write with `ifGenerationMatch=<generation>`. On precondition failure (412), fail with the same error message.
- **Azure**: Acquire a 30-second lease on the ACTIVE blob before writing. If the lease cannot be acquired (409 Conflict), fail with the same error message. Release the lease after writing.

If the target does not support conditional writes (future targets), document this as a known limitation and advise against concurrent applies.

---

## 8) Excludes & Security

**Always excluded (cannot be disabled):**

- `.git/`
- `.env`, `.env.*` (allow `.env.example`, `.env.template`)
- `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`
- `id_rsa`, `id_ed25519`
- `.aws/`, `.ssh/`

**Always excluded (convenience):**

- `node_modules/`, `.venv/`, `__pycache__/`, `.DS_Store`, `Thumbs.db`, `.terraform/`, `*.tfstate*`

User `exclude` is additive and uses gitignore-style globs.

**Symlinks:**

Default: symlinks must resolve within `source_dir`. Otherwise fail plan with error. Optional override:

```hcl
allow_external_symlinks = true
```

Hashing uses raw bytes, no line-ending normalization (see §2.4).

---

## 9) Resources

### 9.1 agentctx_skill

```hcl
resource "agentctx_skill" "pipeline_ner" {
  source_dir = "./skills/pipeline-ner"

  # Required if multiple targets and provider doesn't set default_targets
  targets = ["shared_s3", "shared_azure"]

  exclude = ["*.log", "test/"]

  prune_deployments  = true       # default true; prune runs on every apply, not just destroy
  retain_deployments = 5          # default 5 (per target); only for deployments created by TF

  allow_external_symlinks = false

  # When true, runs all validation (hash computation, exclude processing,
  # manifest generation) without uploading to any target. Useful for CI
  # dry-run checks. Default: false.
  validate_only = false

  # Controls deletion behavior on terraform destroy
  force_destroy = false            # default false; see §11.1
  force_destroy_shared_prefix = false  # default false; see §11.1

  anthropic {
    enabled       = true
    register      = true
    display_title = "Biopharma Pipeline NER"

    # Whether source changes automatically create new Anthropic versions.
    # Only meaningful when canonical_store = "source" and register = true.
    # Default: true.
    auto_version = true

    # Only meaningful when provider canonical_store = "anthropic"
    version_strategy = "auto"       # "auto" | "pinned" | "manual"

    # Required when version_strategy = "pinned" or "manual".
    # For "manual", reference an agentctx_skill_version resource:
    #   pinned_version = agentctx_skill_version.ner_v3.version
    pinned_version = null
  }

  tags = {
    team = "data-engineering"
  }
}
```

**Computed attributes:**

- `skill_name` — from SKILL.md
- `source_hash` — computed per §2.4
- `bundle_hash` — hash of what was actually deployed
- `registry_state` (if Anthropic enabled): `{ skill_id, deployed_version, latest_version }`
- `target_states` — map of target → `{ active_deployment_id, staged_deployment_id, deployed_bundle_hash, last_synced_at }`

**Core behaviors by canonical store:**

**`canonical_store = "source"`:**

- Deploy bytes directly from `source_dir` to each target under a new `deployment_id`
- If Anthropic enabled + register:
  - Create/update skill
  - If `auto_version = true` (default): create a new Anthropic version on each source change
  - If `auto_version = false`: register the skill but do not create versions automatically
- Targets always reflect repo bytes

**`canonical_store = "anthropic"`:**

- If `version_strategy = "auto"`:
  - Source change → create new Anthropic version
  - Download that version bundle
  - Verify per-file integrity (see §9.3)
  - Deploy that bundle's bytes to targets
- If `version_strategy = "pinned"`:
  - Download pinned bundle
  - Verify per-file integrity
  - Deploy pinned bytes to targets (source may differ)
- If `version_strategy = "manual"`:
  - Requires `pinned_version` to be set, typically referencing an `agentctx_skill_version` resource
  - Download the specified version bundle
  - Verify per-file integrity
  - Deploy to targets
  - If `pinned_version` is null/omitted, fail with: `"version_strategy 'manual' requires pinned_version to be set. Reference an agentctx_skill_version resource."`

### 9.2 agentctx_skill_version (Anthropic-only)

Creates a version from a `source_dir`. Used for manual promotion pipelines.

```hcl
resource "agentctx_skill_version" "ner_v3" {
  skill_id   = agentctx_skill.pipeline_ner.registry_state.skill_id
  source_dir = "./skills/pipeline-ner-v3"
}
```

Computed: `version`, `bundle_hash`, `created_at`.

Usage with manual strategy:

```hcl
resource "agentctx_skill" "pipeline_ner" {
  source_dir = "./skills/pipeline-ner"

  anthropic {
    enabled          = true
    register         = true
    display_title    = "Biopharma Pipeline NER"
    version_strategy = "manual"
    pinned_version   = agentctx_skill_version.ner_v3.version
  }
}
```

### 9.3 Bundle Download Integrity Verification

When deploying from Anthropic (`canonical_store = "anthropic"`), the provider must verify bundle integrity before deploying to any target:

1. Download the version bundle from Anthropic API.
2. Compute per-file SHA-256 hashes over the downloaded bytes.
3. Compute the overall `bundle_hash` per §2.4 using the downloaded files.
4. Compare each per-file hash against the version metadata from Anthropic (if available).
5. Compare the computed `bundle_hash` against the expected `registry.bundle_hash`.
6. If any hash mismatch is detected, fail the apply with: `"Bundle integrity check failed for version <version>. Expected bundle_hash <expected>, got <actual>. File-level mismatches: <list>. The download may be corrupted. Retry or investigate."`
7. Only after verification passes, proceed to deploy to targets.

---

## 10) Drift Detection

Per target, refresh does:

1. Read `ACTIVE`
2. Read active manifest
3. Compare `manifest.bundle_hash` to state's `target_states[target].deployed_bundle_hash`
4. If mismatch:
   - Mark drift in state
   - On plan/apply, emit diagnostics with file-level changes by comparing:
     - Local computed file hashes (source-canonical), or
     - Downloaded bundle file hashes (anthropic-canonical)
     - vs `manifest.files`

**Scope and limitations:**

- Drift detection operates at the **manifest level**. If an external actor modifies individual files within a deployment prefix without updating the manifest, the provider will **not** detect this as drift. This is by design: the commit protocol treats the manifest as the source of truth for a deployment. Detecting per-file tampering would require downloading or HEAD-checking every file on every refresh, which is prohibitively expensive.
- Optional: `deep_drift_check = true` on the resource enables per-file verification during refresh. When enabled, the provider reads the active manifest's `files` map and issues `HEAD` requests for each file to verify existence (not content). This catches deletions but not content modifications. Content verification would require downloading files and is not supported in v0.1.

**Eventual consistency considerations:**

- **S3**: Provides strong read-after-write consistency since December 2020. No special handling needed.
- **GCS**: Provides strong consistency for all operations. No special handling needed.
- **Azure Blob**: Provides strong consistency. No special handling needed.

If a target is added in the future without strong consistency, the provider must add a configurable delay between ACTIVE write and verification read.

**Anthropic drift (optional, refresh):**

GET skill details and latest version; store `latest_version` for informational drift. This does **not** force a redeploy unless `version_strategy = "pinned"` and the pinned version differs from the deployed version. If `version_strategy = "auto"`, a newer Anthropic version is reported as informational only (logged at INFO level during plan).

---

## 11) Deletion and Retention

### 11.1 Target deletion

**On `terraform destroy`:**

If `force_destroy = false` (default):

- Delete ONLY the deployments created by this resource (tracked in state by `deployment_id`).
- Delete `ACTIVE` if it currently points to one of those deployments.
- Otherwise leave `ACTIVE` intact (it may be managed by another process or Terraform workspace).
- Leave any unrecognized objects under the prefix untouched.

If `force_destroy = true`:

- Check `force_destroy_shared_prefix`:
  - If `false` (default): delete only `<prefix>/<skill_name>/` (scoped to this skill). This is safe even with `force_destroy = true`.
  - If `true`: delete ALL objects under `<prefix>/<skill_name>/`, including any objects not created by Terraform. Required if the prefix contains objects from external processes that should also be cleaned up.
- If `force_destroy = true` and `force_destroy_shared_prefix = false`, the provider deletes all objects under the skill-scoped path but will **not** delete objects outside `<prefix>/<skill_name>/`.

**Behavior of `force_destroy_shared_prefix`:**

When `false` and `force_destroy = true`: the provider lists all objects under `<prefix>/<skill_name>/` and deletes only those whose keys match the `.agentctx/` layout (deployments, manifests, ACTIVE). If unexpected objects exist outside `.agentctx/`, they are logged as warnings but not deleted.

When `true` and `force_destroy = true`: the provider deletes everything under `<prefix>/<skill_name>/` unconditionally. This is a destructive operation and requires explicit opt-in.

### 11.2 Retention and pruning

- `retain_deployments = 5` (default)
- `prune_deployments = true` (default)

**Pruning lifecycle:** Pruning runs on **every successful apply**, not just on destroy. After a new deployment is activated:

1. List all deployment_ids under `.../.agentctx/deployments/` for this target.
2. Filter to only deployments tracked in Terraform state as created by this resource.
3. Exclude the currently active deployment.
4. Sort remaining deployments by timestamp (extracted from deployment_id).
5. If count exceeds `retain_deployments`, delete the oldest deployments beyond the limit.
6. Deletion includes all objects under the deployment prefix (files, manifest).

Deployments not created by this Terraform resource (e.g., created by another workspace or manually) are never pruned.

---

## 12) Anthropic Integration

### 12.1 API assumptions

Skills API endpoints are called under the declared beta header. Exact endpoints remain as documented in the Anthropic API reference.

### 12.2 Remote deletion

Provider-level `anthropic.destroy_remote` default `false`:

- `false`: never delete Anthropic skills or versions on destroy.
- `true`: on `terraform destroy`:
  1. Delete only the versions created by this Terraform resource (tracked in state by version identifier).
  2. After deleting managed versions, check if the skill has any remaining versions via `GET /skills/{skill_id}/versions`.
  3. If the versions list is empty, delete the skill.
  4. If the versions list returns non-empty (another process created a version between the delete and the check), do **not** delete the skill. Log at WARN: `"Skill <skill_id> has versions not managed by Terraform. Skipping skill deletion."`

No "purge unmanaged" in v0.1.

### 12.3 Identity rules

- Never auto-match by `display_title`.
- Existing skills must be attached via `terraform import` by `skill_id`.

---

## 13) Plan Output Format

During `terraform plan`, the provider produces human-readable output for each resource:

**No changes:**

```
# agentctx_skill.pipeline_ner — no changes
  source_hash: sha256:abc123... (unchanged)
  targets: shared_s3, shared_azure (in sync)
```

**Source change (new deployment):**

```
# agentctx_skill.pipeline_ner — will deploy
  source_hash: sha256:abc123... → sha256:def456...
  bundle_hash: sha256:abc123... → sha256:def456...

  file changes:
    ~ SKILL.md                  (modified)
    + scripts/new_helper.py     (added)
    - scripts/old_helper.py     (removed)

  targets:
    shared_s3:    deploy new (dep_20260213T200102Z_6f2c9a1b → dep_20260214T100000Z_a1b2c3d4)
    shared_azure: deploy new (dep_20260213T200102Z_6f2c9a1b → dep_20260214T100000Z_a1b2c3d4)

  anthropic:
    version: v3 → v4 (auto)

  pruning:
    shared_s3:    remove 1 old deployment (dep_20260210T...; retain 5)
    shared_azure: remove 1 old deployment (dep_20260210T...; retain 5)
```

**Drift detected:**

```
# agentctx_skill.pipeline_ner — drift detected, will re-deploy
  target shared_s3:
    ACTIVE points to dep_20260213T200102Z_6f2c9a1b
    expected bundle_hash: sha256:abc123...
    actual bundle_hash:   sha256:xyz789... (DRIFTED)

    file-level drift:
      ~ SKILL.md (content changed externally)

  action: re-deploy from source to restore expected state
```

**Repair needed:**

```
# agentctx_skill.pipeline_ner — repair required
  target shared_s3:
    ACTIVE points to dep_20260213T200102Z_6f2c9a1b
    manifest: MISSING
    action: re-upload manifest and verify files
```

---

## 14) Import

### 14.1 Importing Anthropic skills

```bash
terraform import agentctx_skill.pipeline_ner skill_01AbCdEf...
```

The provider fetches skill metadata and populates `registry_state`. On next plan, it will detect target drift (since no target state exists) and propose deploying to configured targets.

### 14.2 Importing existing target deployments

For pre-existing deployments created outside Terraform:

```bash
terraform import agentctx_skill.pipeline_ner \
  target:shared_s3:dep_20260213T200102Z_6f2c9a1b
```

Import format: `target:<target_name>:<deployment_id>`

The provider reads the manifest at the deployment path, populates `target_states` for that target, and records the deployment_id as created by this resource (eligible for pruning).

If importing multiple targets, run import once per target. The provider merges target states additively.

To import both an Anthropic skill and a target deployment:

```bash
terraform import agentctx_skill.pipeline_ner \
  skill_01AbCdEf...,target:shared_s3:dep_20260213T200102Z_6f2c9a1b
```

Comma-separated compound import format. The provider processes each segment independently.

---

## 15) Implementation Notes

### 15.1 Deploy engine

Single deploy engine handles:

1. Enumerate files (post-exclude)
2. Compute hashes per §2.4
3. Optionally download Anthropic bundle for deploy (with integrity verification per §9.3)
4. Per target: stage objects → manifest → ACTIVE (with conditional write per §7.3)
5. Clean up any previously staged-but-not-activated deployments (per §7.2)
6. Prune old deployments (per §11.2)
7. Concurrency via provider-scoped semaphore

### 15.2 Target interface (primitive)

```go
type Target interface {
  Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error
  Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error)
  Head(ctx context.Context, key string) (ObjectMeta, error)
  Delete(ctx context.Context, key string) error
  List(ctx context.Context, prefix string) ([]ObjectInfo, error)
  ConditionalPut(ctx context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error
}

type PutOptions struct {
  ContentType string
  Metadata    map[string]string
  KMSKeyID    string // interpreted per target type
}

type WriteCondition struct {
  // For S3: ETag; For GCS: Generation; For Azure: LeaseID
  IfMatch    string
  Generation int64
  LeaseID    string
}

type ObjectMeta struct {
  ETag       string
  Generation int64
  Size       int64
}
```

Avoid `WriteDirectory` / `DeleteDirectory` in the interface. The engine implements those using `List` + `Delete`.

The `Head` method is added for integrity verification during drift detection and repair (check file existence without downloading).

---

## 16) Example Workflows

### 16.1 Source-canonical deploy to S3 + Azure (no Anthropic)

```hcl
provider "agentctx" {
  canonical_store = "source"

  target "s3" {
    type   = "s3"
    bucket = "deerfield-ai-platform"
    region = "us-east-1"
    prefix = "agent-context/skills/"
  }

  target "az" {
    type            = "azure"
    storage_account = "..."
    container_name  = "skills"
    prefix          = "agent-context/skills/"
  }
}

resource "agentctx_skill" "x" {
  source_dir = "./skills/x"
  targets    = ["s3", "az"]
}
```

### 16.2 Anthropic-canonical promotion (pinned)

```hcl
provider "agentctx" {
  canonical_store = "anthropic"

  anthropic { api_key = var.anthropic_api_key }

  target "gcs" {
    type   = "gcs"
    bucket = "..."
    prefix = "agent-context/skills/"
  }
}

resource "agentctx_skill" "x" {
  source_dir = "./skills/x"

  anthropic {
    enabled          = true
    register         = true
    display_title    = "X"
    version_strategy = "pinned"
    pinned_version   = "v12"
  }

  targets = ["gcs"]
}
```

### 16.3 Manual promotion pipeline

```hcl
provider "agentctx" {
  canonical_store = "anthropic"

  anthropic { api_key = var.anthropic_api_key }

  target "prod_s3" {
    type   = "s3"
    bucket = "deerfield-ai-platform"
    region = "us-east-1"
    prefix = "agent-context/skills/"
  }
}

resource "agentctx_skill" "pipeline_ner" {
  source_dir = "./skills/pipeline-ner"

  anthropic {
    enabled          = true
    register         = true
    display_title    = "Biopharma Pipeline NER"
    version_strategy = "manual"
    pinned_version   = agentctx_skill_version.ner_v3.version
  }

  targets = ["prod_s3"]
}

resource "agentctx_skill_version" "ner_v3" {
  skill_id   = agentctx_skill.pipeline_ner.registry_state.skill_id
  source_dir = "./skills/pipeline-ner-v3"
}
```

---

## 17) What's Materially Simpler vs the Original Multi-Anything Spec

**Deleted:**

- Local targets and local atomic-rename complexity
- Open-standard-only mode and spec compliance baggage
- "Targets are everything" abstraction complexity
- Filesystem conventions and external delete safety
- Large parts of the resource matrix (agents/settings deferred to v0.2)

**Kept (because necessary):**

- Staging/commit (ACTIVE pointer) for object stores
- Manifest ownership model
- `canonical_store` clarity
- Anthropic registry/versioning semantics
- Safe target defaulting

**Added in v2:**

- Deterministic cross-platform hash algorithm (§2.4)
- Deployment ID uniqueness guarantees (§2.5)
- Conditional writes for concurrent apply safety (§7.3)
- Incomplete staging cleanup (§7.2)
- Bundle download integrity verification (§9.3)
- Per-target encryption, retry, and timeout configuration (§4.1–4.3)
- Content-type inference (§4.4)
- Plan output format (§13)
- Import paths for both Anthropic skills and existing target deployments (§14)
- Explicit pruning lifecycle (§11.2)
- `validate_only` and `force_destroy_shared_prefix` behavior definitions (§9.1, §11.1)
