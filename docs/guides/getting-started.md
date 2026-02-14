---
page_title: "Getting Started with the agentctx Provider"
subcategory: ""
description: |-
  A step-by-step guide to installing, configuring, and deploying your first skill with the agentctx Terraform provider.
---

# Getting Started with the agentctx Provider

This guide walks you through installing the agentctx provider, configuring a storage target, and deploying your first skill bundle.

## Prerequisites

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 0.15.4
- An account and credentials for at least one supported storage backend:
  - **AWS S3** -- AWS credentials configured (via environment variables, `~/.aws/credentials`, or IAM role)
  - **Azure Blob Storage** -- Azure credentials configured (via environment variables, managed identity, or Azure CLI)
  - **Google Cloud Storage** -- Application Default Credentials configured
- (Optional) An [Anthropic API key](https://console.anthropic.com/) for registry integration

## Step 1: Install the Provider

### From Source

```shell
git clone https://github.com/agentctx/terraform-provider-agentctx.git
cd terraform-provider-agentctx
make install
```

This compiles the provider and installs it to `~/.terraform.d/plugins/registry.terraform.io/agentctx/agentctx/0.1.0/{os}_{arch}/`.

### From the Terraform Registry

Add the provider to your `required_providers` block:

```hcl
terraform {
  required_providers {
    agentctx = {
      source  = "agentctx/agentctx"
      version = "~> 0.1"
    }
  }
}
```

## Step 2: Create a Skill Directory

Create a directory with the files that make up your skill:

```shell
mkdir -p skills/hello-world
```

```shell
cat > skills/hello-world/README.md << 'EOF'
# Hello World Skill

A simple example skill for the agentctx provider.
EOF
```

```shell
cat > skills/hello-world/main.py << 'EOF'
def handler(event):
    return {"message": "Hello, World!"}
EOF
```

## Step 3: Configure the Provider

Create a `main.tf` file:

### Option A: S3 Target

```hcl
terraform {
  required_providers {
    agentctx = {
      source = "agentctx/agentctx"
    }
  }
}

provider "agentctx" {
  target {
    name   = "primary"
    type   = "s3"
    bucket = "my-skill-bucket"
    region = "us-east-1"
    prefix = "skills/"
  }
}
```

### Option B: Azure Target

```hcl
provider "agentctx" {
  target {
    name            = "primary"
    type            = "azure"
    storage_account = "myskillstorage"
    container_name  = "skills"
  }
}
```

### Option C: GCS Target

```hcl
provider "agentctx" {
  target {
    name   = "primary"
    type   = "gcs"
    bucket = "my-skill-bucket"
    prefix = "skills/"
  }
}
```

## Step 4: Define the Skill Resource

Add the skill resource to `main.tf`:

```hcl
resource "agentctx_skill" "hello_world" {
  source_dir = "./skills/hello-world"

  tags = {
    environment = "dev"
  }
}

output "skill_name" {
  value = agentctx_skill.hello_world.skill_name
}

output "bundle_hash" {
  value = agentctx_skill.hello_world.bundle_hash
}

output "deployment_id" {
  value = agentctx_skill.hello_world.target_states["primary"].active_deployment_id
}
```

## Step 5: Deploy

```shell
terraform init
terraform plan
terraform apply
```

Terraform will:

1. Scan `./skills/hello-world` and compute a deterministic SHA-256 bundle hash.
2. Upload all files to the configured storage target under a deployment-specific prefix.
3. Write a JSON manifest describing the deployment.
4. Atomically swap the ACTIVE pointer to the new deployment.

## Step 6: Update the Skill

Modify the skill source files:

```shell
echo 'def handler(event):
    return {"message": "Hello, Updated World!"}' > skills/hello-world/main.py
```

Run `terraform plan` to see the changes detected via hash comparison:

```shell
terraform plan
```

Then apply:

```shell
terraform apply
```

The provider will create a new deployment, swap the ACTIVE pointer, and (by default) prune old deployments, retaining the most recent 5.

## Step 7: Destroy

```shell
terraform destroy
```

This removes all managed deployments and the ACTIVE pointer from the storage target.

## Next Steps

- **Multi-target deployment** -- Add multiple `target` blocks and use `default_targets` or per-resource `targets` to replicate skills across regions or cloud providers. See the [provider documentation](../index.md).
- **Anthropic integration** -- Add an `anthropic` block to the provider and enable registry integration on your skill resources. See the [agentctx_skill resource](../resources/skill.md).
- **Explicit versioning** -- Use the [agentctx_skill_version resource](../resources/skill_version.md) for pinned version workflows.
- **Validate-only mode** -- Set `validate_only = true` on a skill resource to validate bundles in CI without deploying.
- **Import existing deployments** -- Use `terraform import` with skill IDs or target deployment IDs. See the [import section](../resources/skill.md#import) of the skill resource documentation.
