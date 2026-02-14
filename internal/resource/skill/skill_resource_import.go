package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/agentctx/terraform-provider-agentctx/internal/engine"
)

// ImportState implements resource.ResourceWithImportState. It supports three
// compound import ID formats and comma-separated combinations thereof:
//
//   - Skill import:   "skill_01AbCdEf..."
//     Sets registry_state.skill_id so the next Read populates full state.
//
//   - Target import:  "target:<target_name>:<deployment_id>"
//     Reads the manifest from the target and populates target_states.
//
//   - Combined:       "skill_01AbCdEf...,target:shared_s3:dep_..."
//     Processes each segment independently.
func (r *SkillResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	segments := strings.Split(req.ID, ",")

	var (
		skillID      string
		targetImports []targetImport
	)

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}

		switch {
		case strings.HasPrefix(seg, "skill_"):
			// Skill import: the entire segment is the skill ID.
			if skillID != "" {
				resp.Diagnostics.AddError(
					"Invalid Import ID",
					"Only one skill ID may be specified in the import string.",
				)
				return
			}
			skillID = seg

		case strings.HasPrefix(seg, "target:"):
			// Target import: "target:<name>:<deploy_id>"
			parts := strings.SplitN(seg, ":", 3)
			if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
				resp.Diagnostics.AddError(
					"Invalid Import ID",
					fmt.Sprintf(
						"Target import segment %q must be in the format \"target:<target_name>:<deployment_id>\".",
						seg,
					),
				)
				return
			}
			targetImports = append(targetImports, targetImport{
				targetName:   parts[1],
				deploymentID: parts[2],
			})

		default:
			resp.Diagnostics.AddError(
				"Invalid Import ID",
				fmt.Sprintf(
					"Unrecognized import segment %q. Expected a skill ID (\"skill_...\") or a target reference (\"target:<name>:<deploy_id>\").",
					seg,
				),
			)
			return
		}
	}

	if skillID == "" && len(targetImports) == 0 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Import ID must contain at least one skill ID or target import segment.",
		)
		return
	}

	// ------------------------------------------------------------------
	// Populate state from import segments.
	// ------------------------------------------------------------------

	// Set the resource ID.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Set source_dir to unknown so the user must supply it in config.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source_dir"), types.StringUnknown())...)

	// Handle skill import.
	if skillID != "" {
		rsVal, rsDiags := types.ObjectValueFrom(ctx, registryStateAttrTypes(), RegistryStateValue{
			SkillID:         types.StringValue(skillID),
			DeployedVersion: types.StringValue(""),
			LatestVersion:   types.StringValue(""),
		})
		resp.Diagnostics.Append(rsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("registry_state"), rsVal)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Handle target imports.
	if len(targetImports) > 0 {
		eng := engine.New(r.providerData.Semaphore)
		targetStates := make(map[string]attr.Value, len(targetImports))

		for _, ti := range targetImports {
			t, ok := r.providerData.Targets[ti.targetName]
			if !ok {
				resp.Diagnostics.AddError(
					"Unknown Target",
					fmt.Sprintf("Target %q referenced in the import ID is not configured in the provider.", ti.targetName),
				)
				return
			}

			// Derive skill name from the deployment ID by reading the
			// manifest from the target. We pass "" for skill name since we
			// do not know it yet; the engine should locate it via the
			// deployment ID.
			result, refreshErr := eng.Refresh(ctx, t, ti.deploymentID, "", false)
			if refreshErr != nil {
				resp.Diagnostics.AddError(
					"Import Refresh Failed",
					fmt.Sprintf("Failed to read deployment %q from target %q: %s", ti.deploymentID, ti.targetName, refreshErr),
				)
				return
			}

			tflog.Info(ctx, "imported target state", map[string]interface{}{
				"target":        ti.targetName,
				"deployment_id": ti.deploymentID,
				"healthy":       result.Healthy,
			})

			bundleHash := ""
			if result.Manifest != nil {
				bundleHash = result.Manifest.BundleHash

				// Set skill_name from the manifest if available.
				if result.Manifest.ResourceName != "" {
					resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("skill_name"), types.StringValue(result.Manifest.ResourceName))...)
				}
			}

			managedIDs, idDiags := types.ListValueFrom(ctx, types.StringType, []string{ti.deploymentID})
			resp.Diagnostics.Append(idDiags...)
			if resp.Diagnostics.HasError() {
				return
			}

			tsVal, tsDiags := types.ObjectValueFrom(ctx, targetStateAttrTypes(), TargetStateValue{
				ActiveDeploymentID: types.StringValue(result.ActiveDeploymentID),
				StagedDeploymentID: types.StringValue(""),
				DeployedBundleHash: types.StringValue(bundleHash),
				LastSyncedAt:       types.StringValue(""),
				ManagedDeployIDs:   managedIDs,
			})
			resp.Diagnostics.Append(tsDiags...)
			if resp.Diagnostics.HasError() {
				return
			}

			targetStates[ti.targetName] = tsVal
		}

		tsMap, tsDiags := types.MapValue(types.ObjectType{AttrTypes: targetStateAttrTypes()}, targetStates)
		resp.Diagnostics.Append(tsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target_states"), tsMap)...)
	}
}

// targetImport is a parsed "target:<name>:<deploy_id>" segment.
type targetImport struct {
	targetName   string
	deploymentID string
}
