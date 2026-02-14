package skillversion

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/agentctx/terraform-provider-agentctx/internal/anthropic"
	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
	"github.com/agentctx/terraform-provider-agentctx/internal/providerdata"
)

// Compile-time interface checks.
var (
	_ resource.Resource              = &SkillVersionResource{}
	_ resource.ResourceWithConfigure = &SkillVersionResource{}
)

// NewSkillVersionResource returns a new resource.Resource for the
// agentctx_skill_version type.
func NewSkillVersionResource() resource.Resource {
	return &SkillVersionResource{}
}

// SkillVersionResource implements the agentctx_skill_version Terraform
// resource. This resource creates an immutable skill version in the Anthropic
// registry from a local source directory. Updates are not supported; changes
// to skill_id or source_dir force replacement.
type SkillVersionResource struct {
	providerData *providerdata.ProviderData
}

// --------------------------------------------------------------------------
// Metadata
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill_version"
}

// --------------------------------------------------------------------------
// Schema
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates an immutable skill version in the Anthropic registry from a local source directory. Changing `skill_id` or `source_dir` forces recreation.",

		Attributes: map[string]schema.Attribute{
			// ---- Required ----
			"skill_id": schema.StringAttribute{
				MarkdownDescription: "Anthropic skill ID to create the version for.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_dir": schema.StringAttribute{
				MarkdownDescription: "Path to the local directory containing the skill source files.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// ---- Computed ----
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the skill version resource (same as the version ID from the Anthropic API).",
				Computed:            true,
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "Version string assigned by the Anthropic registry.",
				Computed:            true,
			},
			"bundle_hash": schema.StringAttribute{
				MarkdownDescription: "Deterministic SHA-256 hash of the bundle uploaded with this version.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the version was created.",
				Computed:            true,
			},
		},
	}
}

// --------------------------------------------------------------------------
// Configure
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	pd, ok := req.ProviderData.(*providerdata.ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *providerdata.ProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.providerData = pd
}

// --------------------------------------------------------------------------
// Create
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SkillVersionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if r.providerData.Anthropic == nil {
		resp.Diagnostics.AddError(
			"Anthropic Client Not Configured",
			"The agentctx_skill_version resource requires the provider anthropic block to be configured.",
		)
		return
	}

	// 1. Scan the source bundle.
	sourceDir := plan.SourceDir.ValueString()
	b, err := bundle.ScanBundle(sourceDir, nil, false)
	if err != nil {
		resp.Diagnostics.AddError("Bundle Scan Failed", fmt.Sprintf("Failed to scan source directory %q: %s", sourceDir, err))
		return
	}

	// 2. Create the version in the Anthropic registry.
	skillID := plan.SkillID.ValueString()
	tflog.Info(ctx, "creating skill version", map[string]interface{}{
		"skill_id":    skillID,
		"bundle_hash": b.BundleHash,
	})

	ver, createErr := r.providerData.Anthropic.CreateVersion(ctx, skillID, sourceDir)
	if createErr != nil {
		resp.Diagnostics.AddError("Create Version Failed", fmt.Sprintf("Failed to create version for skill %q: %s", skillID, createErr))
		return
	}

	// 3. Save state.
	plan.ID = types.StringValue(ver.ID)
	plan.Version = types.StringValue(ver.Version)
	plan.BundleHash = types.StringValue(b.BundleHash)
	plan.CreatedAt = types.StringValue(ver.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Read
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SkillVersionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if r.providerData.Anthropic == nil {
		// If the anthropic client is not configured, we cannot read the
		// version. Leave state as-is; the next plan will flag the issue.
		return
	}

	skillID := state.SkillID.ValueString()
	versionStr := state.Version.ValueString()

	ver, err := r.providerData.Anthropic.GetVersion(ctx, skillID, versionStr)
	if err != nil {
		// If the version no longer exists, remove from state so Terraform
		// knows it needs to be recreated.
		var apiErr *anthropic.APIError
		if isAPINotFound(err, &apiErr) {
			tflog.Info(ctx, "skill version not found in Anthropic registry, removing from state", map[string]interface{}{
				"skill_id": skillID,
				"version":  versionStr,
			})
			resp.State.RemoveResource(ctx)
			return
		}

		resp.Diagnostics.AddError("Read Version Failed", fmt.Sprintf("Failed to read version %q for skill %q: %s", versionStr, skillID, err))
		return
	}

	// Update state with current values. BundleHash is not returned by the API,
	// so we preserve the value already in state (set during Create).
	state.Version = types.StringValue(ver.Version)
	state.CreatedAt = types.StringValue(ver.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// --------------------------------------------------------------------------
// Update (not supported -- ForceNew on both required attributes)
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"agentctx_skill_version does not support in-place updates. Changes to skill_id or source_dir force replacement.",
	)
}

// --------------------------------------------------------------------------
// Delete
// --------------------------------------------------------------------------

func (r *SkillVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SkillVersionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only delete the remote version if the provider is configured for
	// destroy_remote.
	if r.providerData.Anthropic != nil && r.providerData.Anthropic.DestroyRemote() {
		skillID := state.SkillID.ValueString()
		versionStr := state.Version.ValueString()

		tflog.Info(ctx, "deleting skill version from Anthropic registry", map[string]interface{}{
			"skill_id": skillID,
			"version":  versionStr,
		})

		if err := r.providerData.Anthropic.DeleteVersion(ctx, skillID, versionStr); err != nil {
			var apiErr *anthropic.APIError
			// If already gone, suppress the error.
			if !isAPINotFound(err, &apiErr) {
				resp.Diagnostics.AddError(
					"Delete Version Failed",
					fmt.Sprintf("Failed to delete version %q for skill %q: %s", versionStr, skillID, err),
				)
				return
			}
		}
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// isAPINotFound checks whether err wraps an anthropic.APIError with a 404
// status code. If it does, it sets target to point at the error and returns
// true.
func isAPINotFound(err error, target **anthropic.APIError) bool {
	if err == nil {
		return false
	}
	var apiErr *anthropic.APIError
	if errors.As(err, &apiErr) {
		*target = apiErr
		return apiErr.StatusCode == 404
	}
	return false
}
