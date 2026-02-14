package skill

import "github.com/hashicorp/terraform-plugin-framework/types"

// SkillResourceModel maps the agentctx_skill resource schema to a Go struct.
type SkillResourceModel struct {
	// Config
	SourceDir                types.String          `tfsdk:"source_dir"`
	Targets                  types.List            `tfsdk:"targets"`                    // optional list of strings
	Exclude                  types.List            `tfsdk:"exclude"`                    // optional list of strings
	PruneDeployments         types.Bool            `tfsdk:"prune_deployments"`          // default true
	RetainDeployments        types.Int64           `tfsdk:"retain_deployments"`         // default 5
	AllowExternalSymlinks    types.Bool            `tfsdk:"allow_external_symlinks"`    // default false
	ValidateOnly             types.Bool            `tfsdk:"validate_only"`              // default false
	ForceDestroy             types.Bool            `tfsdk:"force_destroy"`              // default false
	ForceDestroySharedPrefix types.Bool            `tfsdk:"force_destroy_shared_prefix"` // default false
	DeepDriftCheck           types.Bool            `tfsdk:"deep_drift_check"`           // default false
	Tags                     types.Map             `tfsdk:"tags"`                       // optional map of strings
	Anthropic                []AnthropicBlockModel `tfsdk:"anthropic"`                  // optional block, max 1

	// Computed
	ID            types.String `tfsdk:"id"`
	SkillName     types.String `tfsdk:"skill_name"`
	SourceHash    types.String `tfsdk:"source_hash"`
	BundleHash    types.String `tfsdk:"bundle_hash"`
	RegistryState types.Object `tfsdk:"registry_state"`
	TargetStates  types.Map    `tfsdk:"target_states"`
}

// AnthropicBlockModel maps the optional anthropic {} block inside the
// agentctx_skill resource. At most one block may be specified.
type AnthropicBlockModel struct {
	Enabled         types.Bool   `tfsdk:"enabled"`          // default false
	Register        types.Bool   `tfsdk:"register"`         // default true
	DisplayTitle    types.String `tfsdk:"display_title"`    // optional
	AutoVersion     types.Bool   `tfsdk:"auto_version"`     // default true
	VersionStrategy types.String `tfsdk:"version_strategy"` // default "auto"
	PinnedVersion   types.String `tfsdk:"pinned_version"`   // optional
}

// RegistryStateValue represents the computed registry_state nested object.
type RegistryStateValue struct {
	SkillID         types.String `tfsdk:"skill_id"`
	DeployedVersion types.String `tfsdk:"deployed_version"`
	LatestVersion   types.String `tfsdk:"latest_version"`
}

// TargetStateValue represents a single entry in the computed target_states
// map. Each key is a target name, and the value describes the deployment
// state for that target.
type TargetStateValue struct {
	ActiveDeploymentID types.String `tfsdk:"active_deployment_id"`
	StagedDeploymentID types.String `tfsdk:"staged_deployment_id"`
	DeployedBundleHash types.String `tfsdk:"deployed_bundle_hash"`
	LastSyncedAt       types.String `tfsdk:"last_synced_at"`
	ManagedDeployIDs   types.List   `tfsdk:"managed_deploy_ids"` // list of strings
}
