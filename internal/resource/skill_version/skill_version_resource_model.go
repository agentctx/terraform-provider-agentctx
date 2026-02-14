package skillversion

import "github.com/hashicorp/terraform-plugin-framework/types"

// SkillVersionResourceModel maps the agentctx_skill_version resource schema
// to a Go struct.
type SkillVersionResourceModel struct {
	// Required
	SkillID   types.String `tfsdk:"skill_id"`
	SourceDir types.String `tfsdk:"source_dir"`

	// Computed
	ID         types.String `tfsdk:"id"`
	Version    types.String `tfsdk:"version"`
	BundleHash types.String `tfsdk:"bundle_hash"`
	CreatedAt  types.String `tfsdk:"created_at"`
}
