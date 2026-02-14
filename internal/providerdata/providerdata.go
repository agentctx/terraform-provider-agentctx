// Package providerdata defines the ProviderData struct that is shared between
// the provider and its resources / data sources. It is separated into its own
// package to avoid import cycles (provider -> resource -> provider).
package providerdata

import (
	"github.com/agentctx/terraform-provider-agentctx/internal/anthropic"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/sync/semaphore"
)

// ProviderData is configured during provider.Configure() and shared with
// resources via resp.ResourceData and resp.DataSourceData.
type ProviderData struct {
	CanonicalStore string
	DefaultTargets []string
	Targets        map[string]target.Target
	TargetConfigs  map[string]TargetConfigModel
	Anthropic      *anthropic.Client
	Semaphore      *semaphore.Weighted
}

// TargetConfigModel maps each target {} block in the provider configuration.
type TargetConfigModel struct {
	Name            types.String `tfsdk:"name"`
	Type            types.String `tfsdk:"type"`
	Bucket          types.String `tfsdk:"bucket"`
	Region          types.String `tfsdk:"region"`
	KMSKeyID        types.String `tfsdk:"kms_key_id"`
	StorageAccount  types.String `tfsdk:"storage_account"`
	ContainerName   types.String `tfsdk:"container_name"`
	EncryptionScope types.String `tfsdk:"encryption_scope"`
	KMSKeyName      types.String `tfsdk:"kms_key_name"`
	Prefix          types.String `tfsdk:"prefix"`
	MaxConcurrency  types.Int64  `tfsdk:"max_concurrency"`
	MaxRetries      types.Int64  `tfsdk:"max_retries"`
	TimeoutSeconds  types.Int64  `tfsdk:"timeout_seconds"`
	RetryBackoff    types.String `tfsdk:"retry_backoff"`
}
