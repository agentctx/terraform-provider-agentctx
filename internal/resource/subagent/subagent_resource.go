package subagent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"
)

// namePattern validates sub-agent names: lowercase letters, numbers, and
// hyphens, starting and ending with a letter or number.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Compile-time interface checks.
var (
	_ resource.Resource = &SubagentResource{}
)

// NewSubagentResource returns a new resource.Resource for the
// agentctx_subagent type.
func NewSubagentResource() resource.Resource {
	return &SubagentResource{}
}

// SubagentResource implements the agentctx_subagent Terraform resource.
// It generates a Claude Code sub-agent markdown file (YAML frontmatter +
// system prompt) and writes it to a local directory.
type SubagentResource struct{}

// --------------------------------------------------------------------------
// Metadata
// --------------------------------------------------------------------------

func (r *SubagentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subagent"
}

// --------------------------------------------------------------------------
// Schema
// --------------------------------------------------------------------------

func (r *SubagentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Claude Code sub-agent definition file. Generates a Markdown file with YAML frontmatter that conforms to the Claude Code sub-agent specification and writes it to a local directory.",

		Attributes: map[string]schema.Attribute{
			// ---- Required ----
			"name": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the sub-agent. Must use lowercase letters, numbers, and hyphens (e.g. `code-reviewer`).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						namePattern,
						"must contain only lowercase letters, numbers, and hyphens, and must start and end with a letter or number",
					),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Describes when Claude should delegate to this sub-agent. Claude uses this to decide automatic delegation.",
				Required:            true,
			},
			"output_dir": schema.StringAttribute{
				MarkdownDescription: "Directory where the sub-agent markdown file will be written. Typically `.claude/agents` for project-level agents or a plugin `agents/` directory.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"prompt": schema.StringAttribute{
				MarkdownDescription: "The system prompt for the sub-agent, written as the Markdown body after the YAML frontmatter.",
				Required:            true,
			},

			// ---- Optional ----
			"model": schema.StringAttribute{
				MarkdownDescription: "Model the sub-agent uses. Valid values: `sonnet`, `opus`, `haiku`, `inherit`. Defaults to `inherit` (same model as the main conversation).",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("sonnet", "opus", "haiku", "inherit"),
				},
			},
			"tools": schema.ListAttribute{
				MarkdownDescription: "Tools the sub-agent can use. Inherits all tools from the main conversation if omitted. Supports `Task(agent_type)` syntax for restricting spawnable sub-agents.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"disallowed_tools": schema.ListAttribute{
				MarkdownDescription: "Tools to deny, removed from the inherited or specified tool list.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"permission_mode": schema.StringAttribute{
				MarkdownDescription: "Controls how the sub-agent handles permission prompts. Valid values: `default`, `acceptEdits`, `delegate`, `dontAsk`, `bypassPermissions`, `plan`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("default", "acceptEdits", "delegate", "dontAsk", "bypassPermissions", "plan"),
				},
			},
			"max_turns": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of agentic turns before the sub-agent stops.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"skills": schema.ListAttribute{
				MarkdownDescription: "Skills to preload into the sub-agent's context at startup. The full skill content is injected, not just made available for invocation.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"memory": schema.StringAttribute{
				MarkdownDescription: "Persistent memory scope for cross-session learning. Valid values: `user`, `project`, `local`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("user", "project", "local"),
				},
			},

			// ---- Computed ----
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the resource, derived from the output file path.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"content": schema.StringAttribute{
				MarkdownDescription: "The rendered Markdown content of the sub-agent file (YAML frontmatter + prompt).",
				Computed:            true,
			},
			"file_path": schema.StringAttribute{
				MarkdownDescription: "Absolute path to the generated sub-agent markdown file.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"content_hash": schema.StringAttribute{
				MarkdownDescription: "SHA-256 hash of the rendered file content, prefixed with `sha256:`.",
				Computed:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"mcp_server": schema.ListNestedBlock{
				MarkdownDescription: "MCP servers available to this sub-agent. Each entry is either a server name referencing an already-configured server or an inline definition.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Server name. If only `name` is set, it references an already-configured MCP server.",
							Required:            true,
						},
						"command": schema.StringAttribute{
							MarkdownDescription: "Command to start the MCP server for inline definitions.",
							Optional:            true,
						},
						"args": schema.ListAttribute{
							MarkdownDescription: "Arguments for the MCP server command.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"env": schema.MapAttribute{
							MarkdownDescription: "Environment variables for the MCP server process.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"url": schema.StringAttribute{
							MarkdownDescription: "URL for a remote MCP server (SSE transport).",
							Optional:            true,
						},
					},
				},
			},
			"hooks": schema.ListNestedBlock{
				MarkdownDescription: "Lifecycle hooks scoped to this sub-agent. At most one block may be specified.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(1),
				},
				NestedObject: schema.NestedBlockObject{
					Blocks: map[string]schema.Block{
						"pre_tool_use":  hookMatcherBlockSchema("Hook matchers that run before the sub-agent uses a tool."),
						"post_tool_use": hookMatcherBlockSchema("Hook matchers that run after the sub-agent uses a tool."),
						"stop":          hookMatcherBlockSchema("Hook matchers that run when the sub-agent finishes."),
					},
				},
			},
		},
	}
}

// hookMatcherBlockSchema returns the schema for a hook event type (PreToolUse,
// PostToolUse, Stop). Each contains a list of matcher entries with hooks.
func hookMatcherBlockSchema(description string) schema.ListNestedBlock {
	return schema.ListNestedBlock{
		MarkdownDescription: description,
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"matcher": schema.StringAttribute{
					MarkdownDescription: "Regex pattern to match tool names. If omitted, the hook matches all tools.",
					Optional:            true,
				},
			},
			Blocks: map[string]schema.Block{
				"hook": schema.ListNestedBlock{
					MarkdownDescription: "Hook commands to execute when the matcher matches.",
					NestedObject: schema.NestedBlockObject{
						Attributes: map[string]schema.Attribute{
							"type": schema.StringAttribute{
								MarkdownDescription: "Hook type. Currently only `command` is supported.",
								Required:            true,
								Validators: []validator.String{
									stringvalidator.OneOf("command"),
								},
							},
							"command": schema.StringAttribute{
								MarkdownDescription: "Shell command to execute.",
								Required:            true,
							},
						},
					},
				},
			},
		},
	}
}

// --------------------------------------------------------------------------
// Create
// --------------------------------------------------------------------------

func (r *SubagentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SubagentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	content, diags := r.renderContent(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	filePath, err := r.writeFile(ctx, &plan, content)
	if err != nil {
		resp.Diagnostics.AddError("File Write Failed", fmt.Sprintf("Failed to write sub-agent file: %s", err))
		return
	}

	hash := computeHash(content)

	plan.ID = types.StringValue(filePath)
	plan.Content = types.StringValue(content)
	plan.FilePath = types.StringValue(filePath)
	plan.ContentHash = types.StringValue(hash)

	tflog.Info(ctx, "created sub-agent file", map[string]interface{}{
		"name":      plan.Name.ValueString(),
		"file_path": filePath,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Read
// --------------------------------------------------------------------------

func (r *SubagentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SubagentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filePath := state.FilePath.ValueString()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			tflog.Info(ctx, "sub-agent file not found on disk, removing from state", map[string]interface{}{
				"file_path": filePath,
			})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("File Read Failed", fmt.Sprintf("Failed to read sub-agent file %q: %s", filePath, err))
		return
	}

	diskContent := string(data)
	diskHash := computeHash(diskContent)

	state.Content = types.StringValue(diskContent)
	state.ContentHash = types.StringValue(diskHash)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// --------------------------------------------------------------------------
// Update
// --------------------------------------------------------------------------

func (r *SubagentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SubagentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	content, diags := r.renderContent(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	filePath, err := r.writeFile(ctx, &plan, content)
	if err != nil {
		resp.Diagnostics.AddError("File Write Failed", fmt.Sprintf("Failed to write sub-agent file: %s", err))
		return
	}

	hash := computeHash(content)

	plan.ID = types.StringValue(filePath)
	plan.Content = types.StringValue(content)
	plan.FilePath = types.StringValue(filePath)
	plan.ContentHash = types.StringValue(hash)

	tflog.Info(ctx, "updated sub-agent file", map[string]interface{}{
		"name":      plan.Name.ValueString(),
		"file_path": filePath,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Delete
// --------------------------------------------------------------------------

func (r *SubagentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SubagentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filePath := state.FilePath.ValueString()

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		resp.Diagnostics.AddError("File Delete Failed", fmt.Sprintf("Failed to delete sub-agent file %q: %s", filePath, err))
		return
	}

	tflog.Info(ctx, "deleted sub-agent file", map[string]interface{}{
		"name":      state.Name.ValueString(),
		"file_path": filePath,
	})
}

// --------------------------------------------------------------------------
// Rendering
// --------------------------------------------------------------------------

// frontmatter represents the YAML frontmatter of a sub-agent markdown file.
// Field names use yaml tags matching the Claude Code sub-agent specification.
type frontmatter struct {
	Name            string                              `yaml:"name"`
	Description     string                              `yaml:"description"`
	Tools           string                              `yaml:"tools,omitempty"`
	DisallowedTools string                              `yaml:"disallowedTools,omitempty"`
	Model           string                              `yaml:"model,omitempty"`
	PermissionMode  string                              `yaml:"permissionMode,omitempty"`
	MaxTurns        int64                               `yaml:"maxTurns,omitempty"`
	Skills          []string                            `yaml:"skills,omitempty"`
	Memory          string                              `yaml:"memory,omitempty"`
	McpServers      map[string]mcpServerFrontmatter     `yaml:"mcpServers,omitempty"`
	Hooks           map[string][]hookMatcherFrontmatter `yaml:"hooks,omitempty"`
}

// mcpServerFrontmatter represents an MCP server entry in the frontmatter.
type mcpServerFrontmatter struct {
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	URL     string            `yaml:"url,omitempty"`
}

// hookMatcherFrontmatter represents a single hook matcher entry.
type hookMatcherFrontmatter struct {
	Matcher string                 `yaml:"matcher,omitempty"`
	Hooks   []hookEntryFrontmatter `yaml:"hooks"`
}

// hookEntryFrontmatter represents a single hook command.
type hookEntryFrontmatter struct {
	Type    string `yaml:"type"`
	Command string `yaml:"command"`
}

// renderContent builds the full markdown file content from the resource model.
func (r *SubagentResource) renderContent(ctx context.Context, model *SubagentResourceModel) (string, diag.Diagnostics) {
	fm := frontmatter{
		Name:        model.Name.ValueString(),
		Description: model.Description.ValueString(),
	}

	// Tools – comma-separated string
	if !model.Tools.IsNull() && !model.Tools.IsUnknown() {
		var tools []string
		diags := model.Tools.ElementsAs(ctx, &tools, false)
		if diags.HasError() {
			return "", diags
		}
		fm.Tools = strings.Join(tools, ", ")
	}

	// DisallowedTools – comma-separated string
	if !model.DisallowedTools.IsNull() && !model.DisallowedTools.IsUnknown() {
		var disallowed []string
		diags := model.DisallowedTools.ElementsAs(ctx, &disallowed, false)
		if diags.HasError() {
			return "", diags
		}
		fm.DisallowedTools = strings.Join(disallowed, ", ")
	}

	// Simple optional fields
	if !model.Model.IsNull() && !model.Model.IsUnknown() {
		fm.Model = model.Model.ValueString()
	}
	if !model.PermissionMode.IsNull() && !model.PermissionMode.IsUnknown() {
		fm.PermissionMode = model.PermissionMode.ValueString()
	}
	if !model.MaxTurns.IsNull() && !model.MaxTurns.IsUnknown() {
		fm.MaxTurns = model.MaxTurns.ValueInt64()
	}
	if !model.Memory.IsNull() && !model.Memory.IsUnknown() {
		fm.Memory = model.Memory.ValueString()
	}

	// Skills
	if !model.Skills.IsNull() && !model.Skills.IsUnknown() {
		var skills []string
		diags := model.Skills.ElementsAs(ctx, &skills, false)
		if diags.HasError() {
			return "", diags
		}
		fm.Skills = skills
	}

	// MCP Servers
	if len(model.McpServers) > 0 {
		fm.McpServers = make(map[string]mcpServerFrontmatter, len(model.McpServers))
		for _, srv := range model.McpServers {
			entry := mcpServerFrontmatter{}

			if !srv.Command.IsNull() && !srv.Command.IsUnknown() {
				entry.Command = srv.Command.ValueString()
			}
			if !srv.URL.IsNull() && !srv.URL.IsUnknown() {
				entry.URL = srv.URL.ValueString()
			}
			if !srv.Args.IsNull() && !srv.Args.IsUnknown() {
				var args []string
				diags := srv.Args.ElementsAs(ctx, &args, false)
				if diags.HasError() {
					return "", diags
				}
				entry.Args = args
			}
			if !srv.Env.IsNull() && !srv.Env.IsUnknown() {
				env := make(map[string]string)
				diags := srv.Env.ElementsAs(ctx, &env, false)
				if diags.HasError() {
					return "", diags
				}
				entry.Env = env
			}

			fm.McpServers[srv.Name.ValueString()] = entry
		}
	}

	// Hooks
	if len(model.Hooks) > 0 {
		hooks := model.Hooks[0]
		fm.Hooks = make(map[string][]hookMatcherFrontmatter)

		if len(hooks.PreToolUse) > 0 {
			fm.Hooks["PreToolUse"] = convertHookMatchers(hooks.PreToolUse)
		}
		if len(hooks.PostToolUse) > 0 {
			fm.Hooks["PostToolUse"] = convertHookMatchers(hooks.PostToolUse)
		}
		if len(hooks.Stop) > 0 {
			fm.Hooks["Stop"] = convertHookMatchers(hooks.Stop)
		}
	}

	// Marshal frontmatter to YAML
	yamlBytes, err := yaml.Marshal(&fm)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("YAML Marshal Failed", fmt.Sprintf("Failed to marshal sub-agent frontmatter: %s", err))
		return "", diags
	}

	// Build the full markdown content
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(yamlBytes)
	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimSpace(model.Prompt.ValueString()))
	sb.WriteString("\n")

	return sb.String(), nil
}

// convertHookMatchers converts the Terraform model hook matchers to the
// frontmatter representation.
func convertHookMatchers(matchers []HookMatcherModel) []hookMatcherFrontmatter {
	result := make([]hookMatcherFrontmatter, 0, len(matchers))
	for _, m := range matchers {
		entry := hookMatcherFrontmatter{}
		if !m.Matcher.IsNull() && !m.Matcher.IsUnknown() {
			entry.Matcher = m.Matcher.ValueString()
		}
		for _, h := range m.Hooks {
			entry.Hooks = append(entry.Hooks, hookEntryFrontmatter{
				Type:    h.Type.ValueString(),
				Command: h.Command.ValueString(),
			})
		}
		result = append(result, entry)
	}
	return result
}

// --------------------------------------------------------------------------
// File operations
// --------------------------------------------------------------------------

// writeFile writes the rendered content to the output directory and returns
// the absolute file path.
func (r *SubagentResource) writeFile(_ context.Context, model *SubagentResourceModel, content string) (string, error) {
	outputDir := model.OutputDir.ValueString()
	name := model.Name.ValueString()
	fileName := name + ".md"

	// Ensure the output directory exists.
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory %q: %w", outputDir, err)
	}

	filePath := filepath.Join(outputDir, fileName)

	// Resolve to absolute path for consistency.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path for %q: %w", filePath, err)
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing file %q: %w", absPath, err)
	}

	return absPath, nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// computeHash returns the SHA-256 hash of the given content, prefixed with
// "sha256:" to match the convention used elsewhere in the provider.
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("sha256:%x", h)
}
