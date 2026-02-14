package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/sync/semaphore"

	"github.com/agentctx/terraform-provider-agentctx/internal/anthropic"
	skillresource "github.com/agentctx/terraform-provider-agentctx/internal/resource/skill"
	skillversion "github.com/agentctx/terraform-provider-agentctx/internal/resource/skill_version"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// Ensure AgentCtxProvider satisfies the provider.Provider interface.
var _ provider.Provider = &AgentCtxProvider{}

// AgentCtxProvider implements the agentctx Terraform provider.
type AgentCtxProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and run locally.
	version string
}

// New returns a factory function that creates a new AgentCtxProvider instance
// for the given version string. This is the entry-point used in main.go.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AgentCtxProvider{
			version: version,
		}
	}
}

// Metadata returns the provider type name.
func (p *AgentCtxProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "agentctx"
	resp.Version = p.version
}

// Schema returns the provider schema.
func (p *AgentCtxProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The agentctx provider manages agent context skills and their versions across one or more cloud storage targets.",
		Attributes: map[string]schema.Attribute{
			"canonical_store": schema.StringAttribute{
				MarkdownDescription: "Name of the canonical store used for source-of-truth reads. Defaults to `\"source\"` when omitted.",
				Optional:            true,
			},
			"max_concurrency": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of concurrent operations the provider will perform across all targets. Defaults to `16`.",
				Optional:            true,
			},
			"default_targets": schema.ListAttribute{
				MarkdownDescription: "List of target names that resources will replicate to when their own `targets` argument is not set.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
		Blocks: map[string]schema.Block{
			"anthropic": schema.ListNestedBlock{
				MarkdownDescription: "Configuration for the Anthropic API client used for remote skill operations. At most one block may be specified.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"api_key": schema.StringAttribute{
							MarkdownDescription: "Anthropic API key used for authentication. This value is sensitive and will not appear in plan output.",
							Required:            true,
							Sensitive:           true,
						},
						"max_retries": schema.Int64Attribute{
							MarkdownDescription: "Maximum number of retries for failed Anthropic API requests. Defaults to `3`.",
							Optional:            true,
						},
						"destroy_remote": schema.BoolAttribute{
							MarkdownDescription: "Whether to destroy the remote Anthropic resource when the Terraform resource is destroyed. Defaults to `false`.",
							Optional:            true,
						},
						"timeout_seconds": schema.Int64Attribute{
							MarkdownDescription: "Timeout in seconds for individual Anthropic API requests. Defaults to `60`.",
							Optional:            true,
						},
						"base_url": schema.StringAttribute{
							MarkdownDescription: "Override the Anthropic API base URL. Useful for testing with a mock server.",
							Optional:            true,
						},
					},
				},
			},
			"target": schema.ListNestedBlock{
				MarkdownDescription: "Defines a storage target for skill artifacts. At least one target block must be configured.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Unique name used to reference this target in resource configurations and `default_targets`.",
							Required:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Storage backend type. Supported values are `\"s3\"`, `\"azure\"`, and `\"gcs\"`.",
							Required:            true,
						},
						"bucket": schema.StringAttribute{
							MarkdownDescription: "S3 or GCS bucket name. Required for `s3` and `gcs` target types.",
							Optional:            true,
						},
						"region": schema.StringAttribute{
							MarkdownDescription: "AWS region for the S3 bucket. Required for `s3` target type.",
							Optional:            true,
						},
						"kms_key_id": schema.StringAttribute{
							MarkdownDescription: "AWS KMS key ID or ARN used for server-side encryption of S3 objects.",
							Optional:            true,
						},
						"storage_account": schema.StringAttribute{
							MarkdownDescription: "Azure Storage account name. Required for `azure` target type.",
							Optional:            true,
						},
						"container_name": schema.StringAttribute{
							MarkdownDescription: "Azure Blob Storage container name. Required for `azure` target type.",
							Optional:            true,
						},
						"encryption_scope": schema.StringAttribute{
							MarkdownDescription: "Azure encryption scope to apply when writing blobs.",
							Optional:            true,
						},
						"kms_key_name": schema.StringAttribute{
							MarkdownDescription: "GCS Cloud KMS key resource name used for object encryption.",
							Optional:            true,
						},
						"prefix": schema.StringAttribute{
							MarkdownDescription: "Key prefix prepended to all object paths within the target bucket or container.",
							Optional:            true,
						},
						"max_concurrency": schema.Int64Attribute{
							MarkdownDescription: "Maximum number of concurrent operations for this specific target. Overrides the provider-level `max_concurrency`.",
							Optional:            true,
						},
						"max_retries": schema.Int64Attribute{
							MarkdownDescription: "Maximum number of retries for failed operations against this target. Defaults to `3`.",
							Optional:            true,
						},
						"timeout_seconds": schema.Int64Attribute{
							MarkdownDescription: "Timeout in seconds for individual operations against this target. Defaults to `30`.",
							Optional:            true,
						},
						"retry_backoff": schema.StringAttribute{
							MarkdownDescription: "Retry backoff strategy for this target. Supported values are `\"exponential\"` and `\"linear\"`. Defaults to `\"exponential\"`.",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

// Configure parses the provider configuration, validates targets, builds
// target.Target instances and an optional anthropic.Client, and stores
// everything in ProviderData for downstream resources.
func (p *AgentCtxProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config ProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// ----------------------------------------------------------------
	// Resolve top-level defaults
	// ----------------------------------------------------------------
	canonicalStore := "source"
	if !config.CanonicalStore.IsNull() && !config.CanonicalStore.IsUnknown() {
		canonicalStore = config.CanonicalStore.ValueString()
	}

	maxConcurrency := int64(16)
	if !config.MaxConcurrency.IsNull() && !config.MaxConcurrency.IsUnknown() {
		maxConcurrency = config.MaxConcurrency.ValueInt64()
	}

	var defaultTargets []string
	if !config.DefaultTargets.IsNull() && !config.DefaultTargets.IsUnknown() {
		resp.Diagnostics.Append(config.DefaultTargets.ElementsAs(ctx, &defaultTargets, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// ----------------------------------------------------------------
	// Validate and build targets
	// ----------------------------------------------------------------
	if len(config.Targets) == 0 {
		resp.Diagnostics.AddError(
			"Missing Target Configuration",
			"At least one target block must be configured in the provider.",
		)
		return
	}

	targets := make(map[string]target.Target, len(config.Targets))
	targetConfigs := make(map[string]TargetConfigModel, len(config.Targets))

	for _, tc := range config.Targets {
		name := tc.Name.ValueString()
		if name == "" {
			resp.Diagnostics.AddError(
				"Invalid Target Configuration",
				"Every target block must have a non-empty name attribute.",
			)
			return
		}

		if _, exists := targets[name]; exists {
			resp.Diagnostics.AddError(
				"Duplicate Target Name",
				fmt.Sprintf("Target name %q is defined more than once.", name),
			)
			return
		}

		targetType := tc.Type.ValueString()
		if targetType == "" {
			resp.Diagnostics.AddError(
				"Invalid Target Configuration",
				fmt.Sprintf("Target %q must have a non-empty type attribute.", name),
			)
			return
		}

		// Resolve per-target defaults.
		tMaxRetries := int64(3)
		if !tc.MaxRetries.IsNull() && !tc.MaxRetries.IsUnknown() {
			tMaxRetries = tc.MaxRetries.ValueInt64()
		}

		tTimeoutSeconds := int64(30)
		if !tc.TimeoutSeconds.IsNull() && !tc.TimeoutSeconds.IsUnknown() {
			tTimeoutSeconds = tc.TimeoutSeconds.ValueInt64()
		}

		tRetryBackoff := "exponential"
		if !tc.RetryBackoff.IsNull() && !tc.RetryBackoff.IsUnknown() {
			tRetryBackoff = tc.RetryBackoff.ValueString()
		}

		var tMaxConcurrency int64
		if !tc.MaxConcurrency.IsNull() && !tc.MaxConcurrency.IsUnknown() {
			tMaxConcurrency = tc.MaxConcurrency.ValueInt64()
		}

		t, err := target.NewTarget(target.Config{
			Name:            name,
			Type:            targetType,
			Bucket:          tc.Bucket.ValueString(),
			Region:          tc.Region.ValueString(),
			KMSKeyID:        tc.KMSKeyID.ValueString(),
			StorageAccount:  tc.StorageAccount.ValueString(),
			ContainerName:   tc.ContainerName.ValueString(),
			EncryptionScope: tc.EncryptionScope.ValueString(),
			KMSKeyName:      tc.KMSKeyName.ValueString(),
			Prefix:          tc.Prefix.ValueString(),
			MaxConcurrency:  int(tMaxConcurrency),
			MaxRetries:      int(tMaxRetries),
			TimeoutSeconds:  int(tTimeoutSeconds),
			RetryBackoff:    tRetryBackoff,
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"Target Initialization Failed",
				fmt.Sprintf("Failed to create target %q: %s", name, err),
			)
			return
		}

		targets[name] = t
		targetConfigs[name] = tc
	}

	// Validate that every entry in default_targets references a defined target.
	for _, dt := range defaultTargets {
		if _, exists := targets[dt]; !exists {
			resp.Diagnostics.AddError(
				"Invalid Default Target",
				fmt.Sprintf("default_targets references %q which is not defined as a target block.", dt),
			)
			return
		}
	}

	// ----------------------------------------------------------------
	// Optional Anthropic client
	// ----------------------------------------------------------------
	if len(config.Anthropic) > 1 {
		resp.Diagnostics.AddError(
			"Invalid Anthropic Configuration",
			"At most one anthropic block may be specified.",
		)
		return
	}

	var anthropicClient *anthropic.Client
	if len(config.Anthropic) == 1 {
		ac := config.Anthropic[0]

		apiKey := ac.APIKey.ValueString()
		if apiKey == "" {
			resp.Diagnostics.AddError(
				"Invalid Anthropic Configuration",
				"The api_key attribute in the anthropic block must not be empty.",
			)
			return
		}

		aMaxRetries := int64(3)
		if !ac.MaxRetries.IsNull() && !ac.MaxRetries.IsUnknown() {
			aMaxRetries = ac.MaxRetries.ValueInt64()
		}

		aDestroyRemote := false
		if !ac.DestroyRemote.IsNull() && !ac.DestroyRemote.IsUnknown() {
			aDestroyRemote = ac.DestroyRemote.ValueBool()
		}

		aTimeoutSeconds := int64(60)
		if !ac.TimeoutSeconds.IsNull() && !ac.TimeoutSeconds.IsUnknown() {
			aTimeoutSeconds = ac.TimeoutSeconds.ValueInt64()
		}

		var aBaseURL string
		if !ac.BaseURL.IsNull() && !ac.BaseURL.IsUnknown() {
			aBaseURL = ac.BaseURL.ValueString()
		}

		anthropicClient = anthropic.NewClient(anthropic.ClientConfig{
			APIKey:         apiKey,
			BaseURL:        aBaseURL,
			MaxRetries:     int(aMaxRetries),
			DestroyRemote:  aDestroyRemote,
			TimeoutSeconds: int(aTimeoutSeconds),
		})
	}

	// ----------------------------------------------------------------
	// Build ProviderData and share with resources / data sources
	// ----------------------------------------------------------------
	pd := &ProviderData{
		CanonicalStore: canonicalStore,
		DefaultTargets: defaultTargets,
		Targets:        targets,
		TargetConfigs:  targetConfigs,
		Anthropic:      anthropicClient,
		Semaphore:      semaphore.NewWeighted(maxConcurrency),
	}

	resp.DataSourceData = pd
	resp.ResourceData = pd
}

// Resources returns the set of resource types supported by this provider.
func (p *AgentCtxProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		skillresource.NewSkillResource,
		skillversion.NewSkillVersionResource,
	}
}

// DataSources returns the set of data source types supported by this provider.
func (p *AgentCtxProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
