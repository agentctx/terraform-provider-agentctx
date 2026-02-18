package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/agentctx/terraform-provider-agentctx/internal/anthropic"
	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
	"github.com/agentctx/terraform-provider-agentctx/internal/engine"
	"github.com/agentctx/terraform-provider-agentctx/internal/manifest"
	"github.com/agentctx/terraform-provider-agentctx/internal/providerdata"
)

// Compile-time interface checks.
var (
	_ resource.Resource                = &SkillResource{}
	_ resource.ResourceWithConfigure   = &SkillResource{}
	_ resource.ResourceWithModifyPlan  = &SkillResource{}
	_ resource.ResourceWithImportState = &SkillResource{}
)

// NewSkillResource returns a new resource.Resource for the agentctx_skill type.
func NewSkillResource() resource.Resource {
	return &SkillResource{}
}

// SkillResource implements the agentctx_skill Terraform resource.
type SkillResource struct {
	providerData *providerdata.ProviderData
}

// --------------------------------------------------------------------------
// registryStateAttrTypes / targetStateAttrTypes
// --------------------------------------------------------------------------

// registryStateAttrTypes returns the attribute type map for the
// registry_state nested object.
func registryStateAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"skill_id":         types.StringType,
		"deployed_version": types.StringType,
		"latest_version":   types.StringType,
	}
}

// targetStateAttrTypes returns the attribute type map for each entry in the
// target_states map of objects.
func targetStateAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"active_deployment_id": types.StringType,
		"staged_deployment_id": types.StringType,
		"deployed_bundle_hash": types.StringType,
		"last_synced_at":       types.StringType,
		"managed_deploy_ids":   types.ListType{ElemType: types.StringType},
	}
}

// --------------------------------------------------------------------------
// Metadata
// --------------------------------------------------------------------------

func (r *SkillResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

// --------------------------------------------------------------------------
// Schema
// --------------------------------------------------------------------------

func (r *SkillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	emptyListDefault, _ := types.ListValue(types.StringType, []attr.Value{})

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a skill bundle deployed to one or more cloud object storage targets.",

		Attributes: map[string]schema.Attribute{
			// ---- Required ----
			"source_dir": schema.StringAttribute{
				MarkdownDescription: "Path to the local directory containing the skill source files.",
				Required:            true,
			},

			// ---- Optional ----
			"targets": schema.ListAttribute{
				MarkdownDescription: "List of target names to deploy to. When omitted the provider's `default_targets` are used; if those are also empty every configured target is used.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             listdefault.StaticValue(emptyListDefault),
			},
			"exclude": schema.ListAttribute{
				MarkdownDescription: "Additional gitignore-style glob patterns that exclude files from the bundle.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             listdefault.StaticValue(emptyListDefault),
			},
			"prune_deployments": schema.BoolAttribute{
				MarkdownDescription: "Whether to prune old deployments after a successful deploy. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"retain_deployments": schema.Int64Attribute{
				MarkdownDescription: "Number of old deployments to retain when pruning. Defaults to `5`.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(5),
			},
			"allow_external_symlinks": schema.BoolAttribute{
				MarkdownDescription: "Whether to allow symlinks that resolve outside `source_dir`. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"validate_only": schema.BoolAttribute{
				MarkdownDescription: "When `true`, the resource validates the bundle but does not deploy. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"force_destroy": schema.BoolAttribute{
				MarkdownDescription: "Allow destruction of deployments even if the ACTIVE pointer was modified outside Terraform. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"force_destroy_shared_prefix": schema.BoolAttribute{
				MarkdownDescription: "Allow destruction when the storage prefix is shared with other resources. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"deep_drift_check": schema.BoolAttribute{
				MarkdownDescription: "When `true`, Read performs per-file hash checks rather than relying solely on the bundle hash. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"tags": schema.MapAttribute{
				MarkdownDescription: "Arbitrary key-value tags stored in the deployment manifest.",
				Optional:            true,
				ElementType:         types.StringType,
			},

			// ---- Computed ----
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the resource instance.",
				Computed:            true,
			},
			"skill_name": schema.StringAttribute{
				MarkdownDescription: "Derived skill name (base name of `source_dir`).",
				Computed:            true,
			},
			"source_hash": schema.StringAttribute{
				MarkdownDescription: "SHA-256 hash of the source directory structure and metadata.",
				Computed:            true,
			},
			"bundle_hash": schema.StringAttribute{
				MarkdownDescription: "Deterministic SHA-256 hash over all file hashes in the bundle.",
				Computed:            true,
			},
			"registry_state": schema.SingleNestedAttribute{
				MarkdownDescription: "State of the skill in the Anthropic registry (populated only when the `anthropic` block is configured).",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"skill_id": schema.StringAttribute{
						MarkdownDescription: "Anthropic skill identifier.",
						Computed:            true,
					},
					"deployed_version": schema.StringAttribute{
						MarkdownDescription: "Currently deployed version string.",
						Computed:            true,
					},
					"latest_version": schema.StringAttribute{
						MarkdownDescription: "Latest available version string.",
						Computed:            true,
					},
				},
			},
			"target_states": schema.MapNestedAttribute{
				MarkdownDescription: "Per-target deployment state. Keys are target names.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"active_deployment_id": schema.StringAttribute{
							MarkdownDescription: "Deployment ID currently pointed to by the ACTIVE marker.",
							Computed:            true,
						},
						"staged_deployment_id": schema.StringAttribute{
							MarkdownDescription: "Deployment ID staged but not yet promoted to active.",
							Computed:            true,
						},
						"deployed_bundle_hash": schema.StringAttribute{
							MarkdownDescription: "Bundle hash of the active deployment.",
							Computed:            true,
						},
						"last_synced_at": schema.StringAttribute{
							MarkdownDescription: "RFC 3339 timestamp of the last successful sync.",
							Computed:            true,
						},
						"managed_deploy_ids": schema.ListAttribute{
							MarkdownDescription: "List of deployment IDs managed by this resource instance.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},

		Blocks: map[string]schema.Block{
			"anthropic": schema.ListNestedBlock{
				MarkdownDescription: "Configuration for Anthropic registry integration. At most one block may be specified.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Whether Anthropic registry integration is enabled. Defaults to `false`.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(false),
						},
						"register": schema.BoolAttribute{
							MarkdownDescription: "Whether to register the skill with the Anthropic registry on create/update. Defaults to `true`.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"display_title": schema.StringAttribute{
							MarkdownDescription: "Human-readable display title for the skill in the Anthropic registry.",
							Optional:            true,
						},
						"auto_version": schema.BoolAttribute{
							MarkdownDescription: "Whether to automatically create a new version when the bundle changes. Defaults to `true`.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"version_strategy": schema.StringAttribute{
							MarkdownDescription: "Version strategy. Supported values are `\"auto\"` and `\"pinned\"`. Defaults to `\"auto\"`.",
							Optional:            true,
							Computed:            true,
							Default:             stringdefault.StaticString("auto"),
						},
						"pinned_version": schema.StringAttribute{
							MarkdownDescription: "Version string to use when `version_strategy` is `\"pinned\"`. Required when strategy is `\"pinned\"`.",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

// --------------------------------------------------------------------------
// Configure
// --------------------------------------------------------------------------

func (r *SkillResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SkillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SkillResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 1. Resolve targets.
	resolvedTargets, diags := r.resolveTargets(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. Resolve exclude patterns.
	var excludes []string
	resp.Diagnostics.Append(plan.Exclude.ElementsAs(ctx, &excludes, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 3. Scan source bundle.
	sourceDir := plan.SourceDir.ValueString()
	allowExtSym := plan.AllowExternalSymlinks.ValueBool()

	b, err := bundle.ScanBundle(sourceDir, excludes, allowExtSym)
	if err != nil {
		resp.Diagnostics.AddError("Bundle Scan Failed", fmt.Sprintf("Failed to scan source directory %q: %s", sourceDir, err))
		return
	}

	skillName := filepath.Base(sourceDir)
	plan.SkillName = types.StringValue(skillName)
	plan.SourceHash = types.StringValue(b.BundleHash)
	plan.BundleHash = types.StringValue(b.BundleHash)

	// 4. If validate_only, save minimal state and return.
	if plan.ValidateOnly.ValueBool() {
		plan.ID = types.StringValue("validate:" + skillName)
		plan.RegistryState = types.ObjectNull(registryStateAttrTypes())
		plan.TargetStates = types.MapNull(types.ObjectType{AttrTypes: targetStateAttrTypes()})
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	eng := engine.New(r.providerData.Semaphore)

	// 5. Anthropic registry integration.
	var registryInfo *manifest.ManifestRegistry
	registryState := types.ObjectNull(registryStateAttrTypes())

	if len(plan.Anthropic) == 1 && plan.Anthropic[0].Enabled.ValueBool() {
		anthCfg := plan.Anthropic[0]
		if r.providerData.Anthropic == nil {
			resp.Diagnostics.AddError(
				"Anthropic Client Not Configured",
				"The resource anthropic block is enabled but the provider does not have an anthropic block configured.",
			)
			return
		}

		if anthCfg.Register.ValueBool() {
			// Derive display title.
			displayTitle := skillName
			if !anthCfg.DisplayTitle.IsNull() && !anthCfg.DisplayTitle.IsUnknown() {
				displayTitle = anthCfg.DisplayTitle.ValueString()
			}

			// Create skill in the Anthropic registry.
			skill, createErr := r.providerData.Anthropic.CreateSkill(ctx, sourceDir, displayTitle)
			if createErr != nil {
				resp.Diagnostics.AddError("Anthropic Create Skill Failed", fmt.Sprintf("Failed to create skill: %s", createErr))
				return
			}

			registryInfo = &manifest.ManifestRegistry{
				Type:    "anthropic",
				SkillID: skill.ID,
			}

			// Optionally create a version.
			if anthCfg.AutoVersion.ValueBool() {
				ver, verErr := r.providerData.Anthropic.CreateVersion(ctx, skill.ID, sourceDir)
				if verErr != nil {
					resp.Diagnostics.AddError("Anthropic Create Version Failed", fmt.Sprintf("Failed to create version: %s", verErr))
					return
				}

				registryInfo.Version = ver.Version
				registryInfo.BundleHash = b.BundleHash

				rsVal, rsDiags := types.ObjectValueFrom(ctx, registryStateAttrTypes(), RegistryStateValue{
					SkillID:         types.StringValue(skill.ID),
					DeployedVersion: types.StringValue(ver.Version),
					LatestVersion:   types.StringValue(ver.Version),
				})
				resp.Diagnostics.Append(rsDiags...)
				if resp.Diagnostics.HasError() {
					return
				}
				registryState = rsVal
			} else {
				rsVal, rsDiags := types.ObjectValueFrom(ctx, registryStateAttrTypes(), RegistryStateValue{
					SkillID:         types.StringValue(skill.ID),
					DeployedVersion: types.StringValue(""),
					LatestVersion:   types.StringValue(""),
				})
				resp.Diagnostics.Append(rsDiags...)
				if resp.Diagnostics.HasError() {
					return
				}
				registryState = rsVal
			}
		}
	}
	plan.RegistryState = registryState

	// 6. Deploy to each target.
	targetStates := make(map[string]attr.Value, len(resolvedTargets))
	var firstDeployID string
	deployIDByTarget := make(map[string]string, len(resolvedTargets))

	for _, tName := range resolvedTargets {
		t, ok := r.providerData.Targets[tName]
		if !ok {
			resp.Diagnostics.AddError(
				"Target Not Found",
				fmt.Sprintf("Target %q referenced by the resource is not defined in the provider.", tName),
			)
			return
		}

		tflog.Info(ctx, "deploying skill to target", map[string]interface{}{
			"skill_name": skillName,
			"target":     tName,
		})

		result, deployErr := eng.Deploy(ctx, t, engine.DeployInput{
			SkillName:       skillName,
			Bundle:          b,
			CanonicalStore:  r.providerData.CanonicalStore,
			ProviderVersion: "dev",
			ResourceName:    skillName,
			SourceDir:       sourceDir,
			RegistryInfo:    registryInfo,
		})
		if deployErr != nil {
			resp.Diagnostics.AddError(
				"Deployment Failed",
				fmt.Sprintf("Failed to deploy skill %q to target %q: %s", skillName, tName, deployErr),
			)
			return
		}

		if firstDeployID == "" {
			firstDeployID = result.DeploymentID
		}
		deployIDByTarget[tName] = result.DeploymentID

		managedIDs, idDiags := types.ListValueFrom(ctx, types.StringType, []string{result.DeploymentID})
		resp.Diagnostics.Append(idDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		tsVal, tsDiags := types.ObjectValueFrom(ctx, targetStateAttrTypes(), TargetStateValue{
			ActiveDeploymentID: types.StringValue(result.DeploymentID),
			StagedDeploymentID: types.StringValue(""),
			DeployedBundleHash: types.StringValue(result.BundleHash),
			LastSyncedAt:       types.StringValue(time.Now().UTC().Format(time.RFC3339)),
			ManagedDeployIDs:   managedIDs,
		})
		resp.Diagnostics.Append(tsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		targetStates[tName] = tsVal
	}

	tsMap, tsDiags := types.MapValue(types.ObjectType{AttrTypes: targetStateAttrTypes()}, targetStates)
	resp.Diagnostics.Append(tsDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.TargetStates = tsMap

	// 7. Set the resource ID.
	if firstDeployID != "" {
		plan.ID = types.StringValue(skillName + ":" + firstDeployID)
	} else {
		plan.ID = types.StringValue(skillName)
	}

	// 8. Prune old deployments if enabled.
	if plan.PruneDeployments.ValueBool() {
		retain := int(plan.RetainDeployments.ValueInt64())
		for _, tName := range resolvedTargets {
			t := r.providerData.Targets[tName]
			activeDeployID := deployIDByTarget[tName]
			_, pruneErr := eng.Prune(ctx, t, skillName, activeDeployID, []string{activeDeployID}, retain)
			if pruneErr != nil {
				tflog.Warn(ctx, "prune failed", map[string]interface{}{
					"target": tName,
					"error":  pruneErr.Error(),
				})
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Read (refresh)
// --------------------------------------------------------------------------

func (r *SkillResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SkillResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve target list from state.
	resolvedTargets, diags := r.resolveTargets(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	eng := engine.New(r.providerData.Semaphore)

	expectedHash := state.BundleHash.ValueString()
	deepCheck := state.DeepDriftCheck.ValueBool()
	targetStates := make(map[string]attr.Value, len(resolvedTargets))

	for _, tName := range resolvedTargets {
		t, ok := r.providerData.Targets[tName]
		if !ok {
			tflog.Warn(ctx, "target no longer configured, removing from state", map[string]interface{}{
				"target": tName,
			})
			continue
		}

		skillName := state.SkillName.ValueString()
		result, refreshErr := eng.Refresh(ctx, t, skillName, expectedHash, deepCheck)
		if refreshErr != nil {
			resp.Diagnostics.AddError(
				"Refresh Failed",
				fmt.Sprintf("Failed to refresh skill %q from target %q: %s", skillName, tName, refreshErr),
			)
			return
		}

		if result.MissingManifest {
			tflog.Info(ctx, "skill manifest not found on target, resource may have been deleted externally", map[string]interface{}{
				"target": tName,
			})
			resp.State.RemoveResource(ctx)
			return
		}

		// Build the managed deploy IDs list.
		var managedIDs []string
		if result.ActiveDeploymentID != "" {
			managedIDs = append(managedIDs, result.ActiveDeploymentID)
		}

		managedIDsList, idDiags := types.ListValueFrom(ctx, types.StringType, managedIDs)
		resp.Diagnostics.Append(idDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		bundleHash := ""
		if result.Manifest != nil {
			bundleHash = result.Manifest.BundleHash
		}

		tsVal, tsDiags := types.ObjectValueFrom(ctx, targetStateAttrTypes(), TargetStateValue{
			ActiveDeploymentID: types.StringValue(result.ActiveDeploymentID),
			StagedDeploymentID: types.StringValue(""),
			DeployedBundleHash: types.StringValue(bundleHash),
			LastSyncedAt:       types.StringValue(time.Now().UTC().Format(time.RFC3339)),
			ManagedDeployIDs:   managedIDsList,
		})
		resp.Diagnostics.Append(tsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		targetStates[tName] = tsVal

		// Detect drift.
		if result.Drifted {
			tflog.Warn(ctx, "drift detected on target", map[string]interface{}{
				"target":   tName,
				"deployed": bundleHash,
				"expected": expectedHash,
			})
		}
	}

	// Update target_states in state.
	if len(targetStates) > 0 {
		tsMap, tsDiags := types.MapValue(types.ObjectType{AttrTypes: targetStateAttrTypes()}, targetStates)
		resp.Diagnostics.Append(tsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.TargetStates = tsMap
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// --------------------------------------------------------------------------
// Update
// --------------------------------------------------------------------------

func (r *SkillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SkillResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var priorState SkillResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &priorState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 1. Resolve targets.
	resolvedTargets, diags := r.resolveTargets(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. Resolve exclude patterns.
	var excludes []string
	resp.Diagnostics.Append(plan.Exclude.ElementsAs(ctx, &excludes, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 3. Scan source bundle.
	sourceDir := plan.SourceDir.ValueString()
	allowExtSym := plan.AllowExternalSymlinks.ValueBool()

	b, err := bundle.ScanBundle(sourceDir, excludes, allowExtSym)
	if err != nil {
		resp.Diagnostics.AddError("Bundle Scan Failed", fmt.Sprintf("Failed to scan source directory %q: %s", sourceDir, err))
		return
	}

	skillName := filepath.Base(sourceDir)
	plan.SkillName = types.StringValue(skillName)
	plan.SourceHash = types.StringValue(b.BundleHash)
	plan.BundleHash = types.StringValue(b.BundleHash)
	priorSkillName := priorState.SkillName.ValueString()
	cleanupPriorSkill := priorSkillName != "" && priorSkillName != skillName

	// 4. Detect whether the bundle actually changed.
	bundleChanged := priorState.BundleHash.ValueString() != b.BundleHash

	eng := engine.New(r.providerData.Semaphore)

	// 5. Anthropic registry update.
	var registryInfo *manifest.ManifestRegistry
	registryState := priorState.RegistryState

	if len(plan.Anthropic) == 1 && plan.Anthropic[0].Enabled.ValueBool() {
		anthCfg := plan.Anthropic[0]
		if r.providerData.Anthropic == nil {
			resp.Diagnostics.AddError(
				"Anthropic Client Not Configured",
				"The resource anthropic block is enabled but the provider does not have an anthropic block configured.",
			)
			return
		}

		// Extract existing skill_id from registry_state if available.
		var existingSkillID string
		if !priorState.RegistryState.IsNull() && !priorState.RegistryState.IsUnknown() {
			var rsv RegistryStateValue
			resp.Diagnostics.Append(priorState.RegistryState.As(ctx, &rsv, basetypes.ObjectAsOptions{})...)
			if resp.Diagnostics.HasError() {
				return
			}
			existingSkillID = rsv.SkillID.ValueString()
		}

		if anthCfg.Register.ValueBool() {
			displayTitle := skillName
			if !anthCfg.DisplayTitle.IsNull() && !anthCfg.DisplayTitle.IsUnknown() {
				displayTitle = anthCfg.DisplayTitle.ValueString()
			}

			if existingSkillID != "" {
				// Update existing skill.
				_, updateErr := r.providerData.Anthropic.UpdateSkill(ctx, existingSkillID, anthropic.UpdateSkillRequest{
					DisplayTitle: displayTitle,
				})
				if updateErr != nil {
					resp.Diagnostics.AddError("Anthropic Update Skill Failed", fmt.Sprintf("Failed to update skill: %s", updateErr))
					return
				}

				registryInfo = &manifest.ManifestRegistry{
					Type:    "anthropic",
					SkillID: existingSkillID,
				}
			} else {
				// Create new skill.
				skill, createErr := r.providerData.Anthropic.CreateSkill(ctx, sourceDir, displayTitle)
				if createErr != nil {
					resp.Diagnostics.AddError("Anthropic Create Skill Failed", fmt.Sprintf("Failed to create skill: %s", createErr))
					return
				}
				existingSkillID = skill.ID
				registryInfo = &manifest.ManifestRegistry{
					Type:    "anthropic",
					SkillID: skill.ID,
				}
			}

			// Create a new version if the bundle changed and auto_version is on.
			if bundleChanged && anthCfg.AutoVersion.ValueBool() {
				ver, verErr := r.providerData.Anthropic.CreateVersion(ctx, existingSkillID, sourceDir)
				if verErr != nil {
					resp.Diagnostics.AddError("Anthropic Create Version Failed", fmt.Sprintf("Failed to create version: %s", verErr))
					return
				}

				registryInfo.Version = ver.Version
				registryInfo.BundleHash = b.BundleHash

				rsVal, rsDiags := types.ObjectValueFrom(ctx, registryStateAttrTypes(), RegistryStateValue{
					SkillID:         types.StringValue(existingSkillID),
					DeployedVersion: types.StringValue(ver.Version),
					LatestVersion:   types.StringValue(ver.Version),
				})
				resp.Diagnostics.Append(rsDiags...)
				if resp.Diagnostics.HasError() {
					return
				}
				registryState = rsVal
			} else if !bundleChanged {
				// Keep existing registry state.
				registryState = priorState.RegistryState
			}
		}
	} else {
		registryState = types.ObjectNull(registryStateAttrTypes())
	}
	plan.RegistryState = registryState

	// 6. Re-deploy to each target.
	targetStates := make(map[string]attr.Value, len(resolvedTargets))
	var firstDeployID string
	deployIDByTarget := make(map[string]string, len(resolvedTargets))
	managedIDsByTarget := make(map[string][]string, len(resolvedTargets))

	// Read prior target states for previous deploy IDs.
	priorTargetStates := make(map[string]TargetStateValue)
	if !priorState.TargetStates.IsNull() && !priorState.TargetStates.IsUnknown() {
		priorTSMap := make(map[string]types.Object)
		resp.Diagnostics.Append(priorState.TargetStates.ElementsAs(ctx, &priorTSMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for k, v := range priorTSMap {
			var tsv TargetStateValue
			resp.Diagnostics.Append(v.As(ctx, &tsv, basetypes.ObjectAsOptions{})...)
			if resp.Diagnostics.HasError() {
				return
			}
			priorTargetStates[k] = tsv
		}
	}

	resolvedTargetSet := make(map[string]struct{}, len(resolvedTargets))
	for _, tName := range resolvedTargets {
		resolvedTargetSet[tName] = struct{}{}
	}

	// Clean up targets no longer managed by this resource, and clean up the
	// previous skill name if source_dir changed across an update.
	for tName, pts := range priorTargetStates {
		_, stillManaged := resolvedTargetSet[tName]
		if stillManaged && !cleanupPriorSkill {
			continue
		}

		t, ok := r.providerData.Targets[tName]
		if !ok {
			tflog.Warn(ctx, "target no longer configured, skipping cleanup", map[string]interface{}{
				"target": tName,
			})
			continue
		}

		var managedIDs []string
		resp.Diagnostics.Append(pts.ManagedDeployIDs.ElementsAs(ctx, &managedIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		destroySkillName := priorSkillName
		if destroySkillName == "" {
			destroySkillName = skillName
		}
		if cleanupPriorSkill && stillManaged {
			tflog.Info(ctx, "cleaning up previous skill name on target", map[string]interface{}{
				"target":     tName,
				"skill_name": destroySkillName,
				"new_skill":  skillName,
			})
		} else if !stillManaged {
			tflog.Info(ctx, "cleaning up removed target", map[string]interface{}{
				"target":     tName,
				"skill_name": destroySkillName,
			})
		}

		destroyErr := eng.Destroy(ctx, t, destroySkillName, engine.DestroyOptions{
			ForceDestroy:             plan.ForceDestroy.ValueBool(),
			ForceDestroySharedPrefix: plan.ForceDestroySharedPrefix.ValueBool(),
			ManagedDeployIDs:         managedIDs,
			ActiveDeployID:           pts.ActiveDeploymentID.ValueString(),
		})
		if destroyErr != nil {
			resp.Diagnostics.AddError(
				"Cleanup Failed",
				fmt.Sprintf("Failed to clean up skill %q from target %q: %s", destroySkillName, tName, destroyErr),
			)
			return
		}
	}

	for _, tName := range resolvedTargets {
		t, ok := r.providerData.Targets[tName]
		if !ok {
			resp.Diagnostics.AddError(
				"Target Not Found",
				fmt.Sprintf("Target %q is not defined in the provider.", tName),
			)
			return
		}

		// Determine previous deploy ID for conditional writes.
		var prevDeployID string
		if !cleanupPriorSkill {
			if pts, exists := priorTargetStates[tName]; exists {
				prevDeployID = pts.ActiveDeploymentID.ValueString()
			}
		}

		tflog.Info(ctx, "updating skill on target", map[string]interface{}{
			"skill_name": skillName,
			"target":     tName,
		})

		result, deployErr := eng.Deploy(ctx, t, engine.DeployInput{
			SkillName:        skillName,
			Bundle:           b,
			CanonicalStore:   r.providerData.CanonicalStore,
			ProviderVersion:  "dev",
			ResourceName:     skillName,
			SourceDir:        sourceDir,
			RegistryInfo:     registryInfo,
			PreviousDeployID: prevDeployID,
		})
		if deployErr != nil {
			resp.Diagnostics.AddError(
				"Deployment Failed",
				fmt.Sprintf("Failed to deploy skill %q to target %q: %s", skillName, tName, deployErr),
			)
			return
		}

		if firstDeployID == "" {
			firstDeployID = result.DeploymentID
		}
		deployIDByTarget[tName] = result.DeploymentID

		// Merge managed deploy IDs.
		var managedIDs []string
		if !cleanupPriorSkill {
			if pts, exists := priorTargetStates[tName]; exists {
				resp.Diagnostics.Append(pts.ManagedDeployIDs.ElementsAs(ctx, &managedIDs, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
			}
		}
		managedIDs = appendUnique(managedIDs, result.DeploymentID)
		managedIDsByTarget[tName] = managedIDs

		managedIDsList, idDiags := types.ListValueFrom(ctx, types.StringType, managedIDs)
		resp.Diagnostics.Append(idDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		tsVal, tsDiags := types.ObjectValueFrom(ctx, targetStateAttrTypes(), TargetStateValue{
			ActiveDeploymentID: types.StringValue(result.DeploymentID),
			StagedDeploymentID: types.StringValue(""),
			DeployedBundleHash: types.StringValue(result.BundleHash),
			LastSyncedAt:       types.StringValue(time.Now().UTC().Format(time.RFC3339)),
			ManagedDeployIDs:   managedIDsList,
		})
		resp.Diagnostics.Append(tsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		targetStates[tName] = tsVal
	}

	tsMap, tsDiags := types.MapValue(types.ObjectType{AttrTypes: targetStateAttrTypes()}, targetStates)
	resp.Diagnostics.Append(tsDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.TargetStates = tsMap

	// 7. Update resource ID.
	if firstDeployID != "" {
		plan.ID = types.StringValue(skillName + ":" + firstDeployID)
	} else {
		plan.ID = priorState.ID
	}

	// 8. Prune old deployments if enabled.
	if plan.PruneDeployments.ValueBool() {
		retain := int(plan.RetainDeployments.ValueInt64())
		for _, tName := range resolvedTargets {
			t := r.providerData.Targets[tName]
			activeDeployID := deployIDByTarget[tName]
			managedIDs := managedIDsByTarget[tName]
			_, pruneErr := eng.Prune(ctx, t, skillName, activeDeployID, managedIDs, retain)
			if pruneErr != nil {
				tflog.Warn(ctx, "prune failed", map[string]interface{}{
					"target": tName,
					"error":  pruneErr.Error(),
				})
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Delete
// --------------------------------------------------------------------------

func (r *SkillResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SkillResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve targets from state.
	resolvedTargets, diags := r.resolveTargets(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read prior target states.
	priorTargetStates := make(map[string]TargetStateValue)
	if !state.TargetStates.IsNull() && !state.TargetStates.IsUnknown() {
		priorTSMap := make(map[string]types.Object)
		resp.Diagnostics.Append(state.TargetStates.ElementsAs(ctx, &priorTSMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for k, v := range priorTSMap {
			var tsv TargetStateValue
			resp.Diagnostics.Append(v.As(ctx, &tsv, basetypes.ObjectAsOptions{})...)
			if resp.Diagnostics.HasError() {
				return
			}
			priorTargetStates[k] = tsv
		}
	}

	eng := engine.New(r.providerData.Semaphore)
	skillName := state.SkillName.ValueString()

	// 1. Destroy from each target.
	for _, tName := range resolvedTargets {
		t, ok := r.providerData.Targets[tName]
		if !ok {
			tflog.Warn(ctx, "target no longer configured, skipping destroy", map[string]interface{}{
				"target": tName,
			})
			continue
		}

		var managedIDs []string
		var activeDeployID string
		if pts, exists := priorTargetStates[tName]; exists {
			resp.Diagnostics.Append(pts.ManagedDeployIDs.ElementsAs(ctx, &managedIDs, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
			activeDeployID = pts.ActiveDeploymentID.ValueString()
		}

		tflog.Info(ctx, "destroying skill from target", map[string]interface{}{
			"skill_name": skillName,
			"target":     tName,
		})

		destroyErr := eng.Destroy(ctx, t, skillName, engine.DestroyOptions{
			ForceDestroy:             state.ForceDestroy.ValueBool(),
			ForceDestroySharedPrefix: state.ForceDestroySharedPrefix.ValueBool(),
			ManagedDeployIDs:         managedIDs,
			ActiveDeployID:           activeDeployID,
		})
		if destroyErr != nil {
			resp.Diagnostics.AddError(
				"Destroy Failed",
				fmt.Sprintf("Failed to destroy skill %q from target %q: %s", skillName, tName, destroyErr),
			)
			return
		}
	}

	// 2. If anthropic integration is enabled and destroy_remote, delete
	// managed versions first, then delete the skill if no versions remain.
	// Per spec §12.2, the API requires all versions to be deleted before
	// the skill can be deleted.
	if r.providerData.Anthropic != nil && r.providerData.Anthropic.DestroyRemote() {
		if !state.RegistryState.IsNull() && !state.RegistryState.IsUnknown() {
			var rsv RegistryStateValue
			resp.Diagnostics.Append(state.RegistryState.As(ctx, &rsv, basetypes.ObjectAsOptions{})...)
			if resp.Diagnostics.HasError() {
				return
			}

			skillID := rsv.SkillID.ValueString()
			if skillID != "" {
				// Delete all versions managed by this resource.
				versions, listErr := r.providerData.Anthropic.ListVersions(ctx, skillID)
				if listErr != nil {
					tflog.Warn(ctx, "failed to list versions for cleanup", map[string]interface{}{
						"skill_id": skillID,
						"error":    listErr.Error(),
					})
				} else {
					for _, v := range versions {
						tflog.Info(ctx, "deleting skill version from Anthropic registry", map[string]interface{}{
							"skill_id": skillID,
							"version":  v.Version,
						})
						if delErr := r.providerData.Anthropic.DeleteVersion(ctx, skillID, v.Version); delErr != nil {
							tflog.Warn(ctx, "failed to delete version", map[string]interface{}{
								"skill_id": skillID,
								"version":  v.Version,
								"error":    delErr.Error(),
							})
						}
					}
				}

				// Check if any versions remain (created by other processes).
				remaining, remainErr := r.providerData.Anthropic.ListVersions(ctx, skillID)
				if remainErr != nil {
					tflog.Warn(ctx, "failed to check remaining versions", map[string]interface{}{
						"skill_id": skillID,
						"error":    remainErr.Error(),
					})
				} else if len(remaining) > 0 {
					tflog.Warn(ctx, "skill has versions not managed by Terraform, skipping skill deletion", map[string]interface{}{
						"skill_id":           skillID,
						"remaining_versions": len(remaining),
					})
				} else {
					tflog.Info(ctx, "deleting skill from Anthropic registry", map[string]interface{}{
						"skill_id": skillID,
					})
					if err := r.providerData.Anthropic.DeleteSkill(ctx, skillID); err != nil {
						resp.Diagnostics.AddError(
							"Anthropic Delete Skill Failed",
							fmt.Sprintf("Failed to delete skill %q from Anthropic registry: %s", skillID, err),
						)
						return
					}
				}
			}
		}
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// resolveTargets determines the effective list of target names for the
// resource per spec §3.2:
//  1. Explicit resource `targets` attribute
//  2. Provider `default_targets`
//  3. Implicit single target (only if exactly 1 target is configured)
//
// Validation:
//   - `targets = []` (empty list) → validation error
//   - 2+ targets, no default_targets, resource omits targets → validation error
func (r *SkillResource) resolveTargets(ctx context.Context, model SkillResourceModel) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Check explicit targets attribute.
	var explicit []string
	if !model.Targets.IsNull() && !model.Targets.IsUnknown() {
		diags.Append(model.Targets.ElementsAs(ctx, &explicit, false)...)
		if diags.HasError() {
			return nil, diags
		}

		// Per spec §3.2: explicit empty list is a validation error.
		if len(explicit) == 0 {
			// The default is an empty list from the schema, so only error
			// if the user actually wrote `targets = []` vs omitting it.
			// We cannot distinguish these in terraform-plugin-framework with
			// a ListDefault, so we treat empty as "omitted" and fall through.
		} else {
			return explicit, diags
		}
	}

	// Fall back to provider default_targets.
	if len(r.providerData.DefaultTargets) > 0 {
		return r.providerData.DefaultTargets, diags
	}

	// Implicit single target: only if exactly 1 target is configured.
	if len(r.providerData.Targets) == 1 {
		all := make([]string, 0, 1)
		for name := range r.providerData.Targets {
			all = append(all, name)
		}
		return all, diags
	}

	// 2+ targets, no default_targets, resource omits targets → error per §3.2.
	diags.AddError(
		"Ambiguous Target Configuration",
		"Multiple targets are configured in the provider but neither `default_targets` on the provider nor `targets` on the resource is set. "+
			"Set `default_targets` on the provider or specify `targets` on the resource.",
	)
	return nil, diags
}

// appendUnique appends s to the slice only if it is not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
