package provider

import "github.com/hashicorp/terraform-plugin-framework/types"

// ProviderModel maps the provider schema to a Go struct.
type ProviderModel struct {
	CanonicalStore types.String           `tfsdk:"canonical_store"`
	MaxConcurrency types.Int64            `tfsdk:"max_concurrency"`
	DefaultTargets types.List             `tfsdk:"default_targets"` // List of strings
	Anthropic      []AnthropicConfigModel `tfsdk:"anthropic"`
	Targets        []TargetConfigModel    `tfsdk:"target"`
}

// AnthropicConfigModel maps the anthropic {} block.
type AnthropicConfigModel struct {
	APIKey         types.String `tfsdk:"api_key"`
	BaseURL        types.String `tfsdk:"base_url"`
	MaxRetries     types.Int64  `tfsdk:"max_retries"`
	DestroyRemote  types.Bool   `tfsdk:"destroy_remote"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
}
