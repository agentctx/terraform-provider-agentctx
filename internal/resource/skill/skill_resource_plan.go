package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
)

// ModifyPlan implements resource.ResourceWithModifyPlan. It performs
// validation and plan-time computations before Terraform applies changes.
func (r *SkillResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// If the entire resource is being destroyed there is nothing to validate.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan SkillResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// ---------------------------------------------------------------
	// 1. Validate target references exist in the provider config.
	// ---------------------------------------------------------------
	if r.providerData != nil && !plan.Targets.IsNull() && !plan.Targets.IsUnknown() {
		var targetNames []string
		resp.Diagnostics.Append(plan.Targets.ElementsAs(ctx, &targetNames, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		for _, tName := range targetNames {
			if _, exists := r.providerData.Targets[tName]; !exists {
				resp.Diagnostics.AddError(
					"Invalid Target Reference",
					fmt.Sprintf(
						"Target %q is referenced in the resource targets list but is not defined in the provider configuration.",
						tName,
					),
				)
			}
		}

		if resp.Diagnostics.HasError() {
			return
		}
	}

	// ---------------------------------------------------------------
	// 2. Validate version_strategy / pinned_version consistency.
	// ---------------------------------------------------------------
	if len(plan.Anthropic) == 1 {
		anthCfg := plan.Anthropic[0]

		strategy := "auto"
		if !anthCfg.VersionStrategy.IsNull() && !anthCfg.VersionStrategy.IsUnknown() {
			strategy = anthCfg.VersionStrategy.ValueString()
		}

		switch strategy {
		case "auto":
			if !anthCfg.PinnedVersion.IsNull() && !anthCfg.PinnedVersion.IsUnknown() && anthCfg.PinnedVersion.ValueString() != "" {
				resp.Diagnostics.AddError(
					"Invalid Version Configuration",
					"pinned_version must not be set when version_strategy is \"auto\". Either remove pinned_version or set version_strategy to \"pinned\" or \"manual\".",
				)
			}
		case "pinned", "manual":
			if anthCfg.PinnedVersion.IsNull() || anthCfg.PinnedVersion.IsUnknown() || anthCfg.PinnedVersion.ValueString() == "" {
				resp.Diagnostics.AddError(
					"Invalid Version Configuration",
					fmt.Sprintf("pinned_version is required when version_strategy is %q. Reference an agentctx_skill_version resource.", strategy),
				)
			}
		default:
			resp.Diagnostics.AddError(
				"Invalid Version Strategy",
				fmt.Sprintf("version_strategy must be \"auto\", \"pinned\", or \"manual\", got %q.", strategy),
			)
		}

		if resp.Diagnostics.HasError() {
			return
		}
	}

	// ---------------------------------------------------------------
	// 3. Warn if validate_only is set.
	// ---------------------------------------------------------------
	if !plan.ValidateOnly.IsNull() && !plan.ValidateOnly.IsUnknown() && plan.ValidateOnly.ValueBool() {
		resp.Diagnostics.AddWarning(
			"Validate-Only Mode",
			"The resource is configured with validate_only = true. No deployment will be performed; only bundle validation will run during apply.",
		)
	}

	// ---------------------------------------------------------------
	// 4. Compute plan-time source_hash if source_dir is known.
	// ---------------------------------------------------------------
	if !plan.SourceDir.IsNull() && !plan.SourceDir.IsUnknown() {
		sourceDir := plan.SourceDir.ValueString()

		// Only attempt to hash if the directory actually exists on disk
		// during the plan phase. It may not exist in CI plan-only runs.
		absDir, absErr := filepath.Abs(sourceDir)
		if absErr == nil {
			if info, statErr := os.Stat(absDir); statErr == nil && info.IsDir() {
				var excludes []string
				if !plan.Exclude.IsNull() && !plan.Exclude.IsUnknown() {
					resp.Diagnostics.Append(plan.Exclude.ElementsAs(ctx, &excludes, false)...)
					if resp.Diagnostics.HasError() {
						return
					}
				}

				allowExtSym := false
				if !plan.AllowExternalSymlinks.IsNull() && !plan.AllowExternalSymlinks.IsUnknown() {
					allowExtSym = plan.AllowExternalSymlinks.ValueBool()
				}

				b, scanErr := bundle.ScanBundle(sourceDir, excludes, allowExtSym)
				if scanErr != nil {
					tflog.Warn(ctx, "plan-time bundle scan failed, hash will be computed at apply", map[string]interface{}{
						"source_dir": sourceDir,
						"error":      scanErr.Error(),
					})
				} else {
					newHash := b.BundleHash
					plan.SourceHash = types.StringValue(newHash)
					plan.BundleHash = types.StringValue(newHash)
					plan.SkillName = types.StringValue(filepath.Base(sourceDir))

					// On update, if the bundle hash changed, mark
					// mutable computed attributes as unknown so
					// Terraform knows they will change during apply.
					if !req.State.Raw.IsNull() {
						var state SkillResourceModel
						resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
						if resp.Diagnostics.HasError() {
							return
						}

						if state.BundleHash.ValueString() != newHash {
							plan.ID = types.StringUnknown()
							plan.TargetStates = types.MapUnknown(types.ObjectType{AttrTypes: targetStateAttrTypes()})
							plan.RegistryState = types.ObjectUnknown(registryStateAttrTypes())
						}
					}

					resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
				}
			}
		}
	}
}
