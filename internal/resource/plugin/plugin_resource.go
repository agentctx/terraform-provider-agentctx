package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// namePattern validates plugin names: lowercase letters, numbers, and hyphens,
// starting and ending with a letter or number.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Compile-time interface checks.
var _ resource.Resource = &PluginResource{}

// NewPluginResource returns a new resource.Resource for the agentctx_plugin type.
func NewPluginResource() resource.Resource {
	return &PluginResource{}
}

// PluginResource implements the agentctx_plugin Terraform resource. It
// generates a complete Claude Code plugin directory structure including the
// plugin.json manifest, skills, agents, commands, hooks, MCP servers, LSP
// servers, and bundled files.
type PluginResource struct{}

// --------------------------------------------------------------------------
// Metadata
// --------------------------------------------------------------------------

func (r *PluginResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin"
}

// --------------------------------------------------------------------------
// Schema
// --------------------------------------------------------------------------

func (r *PluginResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Claude Code plugin directory structure. Generates the complete plugin layout including the `.claude-plugin/plugin.json` manifest, skills, agents, commands, hooks, MCP server definitions, LSP server configurations, and bundled files.",

		Attributes: map[string]schema.Attribute{
			// ---- Required ----
			"name": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the plugin (kebab-case). Used for namespacing components.",
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
			"output_dir": schema.StringAttribute{
				MarkdownDescription: "Directory where the plugin structure will be generated. The plugin files are written directly into this directory (e.g. `output_dir/.claude-plugin/plugin.json`).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// ---- Optional metadata ----
			"version": schema.StringAttribute{
				MarkdownDescription: "Semantic version of the plugin (e.g. `1.0.0`).",
				Optional:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Brief explanation of the plugin's purpose.",
				Optional:            true,
			},
			"homepage": schema.StringAttribute{
				MarkdownDescription: "URL to the plugin's documentation or homepage.",
				Optional:            true,
			},
			"repository": schema.StringAttribute{
				MarkdownDescription: "URL to the plugin's source code repository.",
				Optional:            true,
			},
			"license": schema.StringAttribute{
				MarkdownDescription: "License identifier (e.g. `MIT`, `Apache-2.0`).",
				Optional:            true,
			},
			"keywords": schema.ListAttribute{
				MarkdownDescription: "Discovery tags for the plugin.",
				Optional:            true,
				ElementType:         types.StringType,
			},

			// ---- Computed ----
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the resource, derived from the output directory.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"plugin_dir": schema.StringAttribute{
				MarkdownDescription: "Absolute path to the generated plugin directory.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"manifest_json": schema.StringAttribute{
				MarkdownDescription: "The rendered plugin.json manifest content.",
				Computed:            true,
			},
			"content_hash": schema.StringAttribute{
				MarkdownDescription: "SHA-256 hash of the manifest content, prefixed with `sha256:`.",
				Computed:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"author": schema.ListNestedBlock{
				MarkdownDescription: "Author information for the plugin. At most one block may be specified.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(1),
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Author name.",
							Required:            true,
						},
						"email": schema.StringAttribute{
							MarkdownDescription: "Author email address.",
							Optional:            true,
						},
						"url": schema.StringAttribute{
							MarkdownDescription: "Author URL (e.g. GitHub profile).",
							Optional:            true,
						},
					},
				},
			},
			"skill": schema.ListNestedBlock{
				MarkdownDescription: "Skills to include in the plugin. Each skill is placed in the `skills/<name>/` directory. Provide either `source_dir` to copy an existing skill directory (e.g. from an `agentctx_skill` resource's source) or `content` to write a `SKILL.md` inline.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Skill name (used as directory name under `skills/`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									namePattern,
									"must contain only lowercase letters, numbers, and hyphens",
								),
							},
						},
						"source_dir": schema.StringAttribute{
							MarkdownDescription: "Path to an existing skill directory to copy. The entire directory contents are copied into `skills/<name>/`. Use this to reference skills managed by `agentctx_skill` resources.",
							Optional:            true,
						},
						"content": schema.StringAttribute{
							MarkdownDescription: "Inline SKILL.md content. Written to `skills/<name>/SKILL.md`.",
							Optional:            true,
						},
					},
				},
			},
			"agent": schema.ListNestedBlock{
				MarkdownDescription: "Agents to include in the plugin. Each agent is placed in the `agents/` directory. Provide either `source_file` to copy from an existing file (e.g. an `agentctx_subagent` resource's `file_path`) or `content` to write the agent markdown inline.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Agent name (used as filename: `agents/<name>.md`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									namePattern,
									"must contain only lowercase letters, numbers, and hyphens",
								),
							},
						},
						"source_file": schema.StringAttribute{
							MarkdownDescription: "Path to an existing agent markdown file to copy. Use this to reference agents managed by `agentctx_subagent` resources via their `file_path` output.",
							Optional:            true,
						},
						"content": schema.StringAttribute{
							MarkdownDescription: "Inline agent markdown content.",
							Optional:            true,
						},
					},
				},
			},
			"command": schema.ListNestedBlock{
				MarkdownDescription: "Commands (slash commands) to include in the plugin. Each command is placed in the `commands/` directory as a markdown file.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Command name (used as filename: `commands/<name>.md`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									namePattern,
									"must contain only lowercase letters, numbers, and hyphens",
								),
							},
						},
						"source_file": schema.StringAttribute{
							MarkdownDescription: "Path to an existing command markdown file to copy.",
							Optional:            true,
						},
						"content": schema.StringAttribute{
							MarkdownDescription: "Inline command markdown content.",
							Optional:            true,
						},
					},
				},
			},
			"mcp_server": schema.ListNestedBlock{
				MarkdownDescription: "MCP server definitions bundled with the plugin. Written to `.mcp.json` in the plugin root.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "MCP server name (used as key in `.mcp.json`).",
							Required:            true,
						},
						"command": schema.StringAttribute{
							MarkdownDescription: "Command to start the MCP server. Use `${CLAUDE_PLUGIN_ROOT}` for paths relative to plugin root.",
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
						"cwd": schema.StringAttribute{
							MarkdownDescription: "Working directory for the MCP server process.",
							Optional:            true,
						},
					},
				},
			},
			"lsp_server": schema.ListNestedBlock{
				MarkdownDescription: "LSP server configurations bundled with the plugin. Written to `.lsp.json` in the plugin root.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "LSP server name (used as key in `.lsp.json`).",
							Required:            true,
						},
						"command": schema.StringAttribute{
							MarkdownDescription: "The LSP binary to execute (must be in PATH).",
							Required:            true,
						},
						"args": schema.ListAttribute{
							MarkdownDescription: "Command-line arguments for the LSP server.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"transport": schema.StringAttribute{
							MarkdownDescription: "Communication transport: `stdio` (default) or `socket`.",
							Optional:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("stdio", "socket"),
							},
						},
						"env": schema.MapAttribute{
							MarkdownDescription: "Environment variables to set when starting the server.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"initialization_options": schema.MapAttribute{
							MarkdownDescription: "Options passed to the server during initialization.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"settings": schema.MapAttribute{
							MarkdownDescription: "Settings passed via `workspace/didChangeConfiguration`.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"extension_to_language": schema.MapAttribute{
							MarkdownDescription: "Maps file extensions to language identifiers (e.g. `{\".go\" = \"go\"}`).",
							Required:            true,
							ElementType:         types.StringType,
						},
						"restart_on_crash": schema.BoolAttribute{
							MarkdownDescription: "Whether to automatically restart the server if it crashes.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(false),
						},
						"max_restarts": schema.Int64Attribute{
							MarkdownDescription: "Maximum number of restart attempts before giving up.",
							Optional:            true,
						},
					},
				},
			},
			"hooks": schema.ListNestedBlock{
				MarkdownDescription: "Hook configurations for the plugin. Written to `hooks/hooks.json`. At most one block may be specified.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(1),
				},
				NestedObject: schema.NestedBlockObject{
					Blocks: map[string]schema.Block{
						"pre_tool_use":         hookEventBlockSchema("Hooks that run before Claude uses a tool."),
						"post_tool_use":        hookEventBlockSchema("Hooks that run after Claude successfully uses a tool."),
						"post_tool_use_failure": hookEventBlockSchema("Hooks that run after a Claude tool execution fails."),
						"user_prompt_submit":   hookEventBlockSchema("Hooks that run when the user submits a prompt."),
						"notification":         hookEventBlockSchema("Hooks that run when Claude Code sends notifications."),
						"stop":                hookEventBlockSchema("Hooks that run when Claude attempts to stop."),
						"subagent_start":       hookEventBlockSchema("Hooks that run when a subagent is started."),
						"subagent_stop":        hookEventBlockSchema("Hooks that run when a subagent attempts to stop."),
						"session_start":        hookEventBlockSchema("Hooks that run at the beginning of sessions."),
						"session_end":          hookEventBlockSchema("Hooks that run at the end of sessions."),
						"pre_compact":          hookEventBlockSchema("Hooks that run before conversation history is compacted."),
					},
				},
			},
			"file": schema.ListNestedBlock{
				MarkdownDescription: "Extra files to bundle into the plugin directory (e.g. scripts, configuration files). Each file is written to the specified path relative to the plugin root.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"path": schema.StringAttribute{
							MarkdownDescription: "Relative path within the plugin directory (e.g. `scripts/format-code.sh`).",
							Required:            true,
						},
						"content": schema.StringAttribute{
							MarkdownDescription: "File content to write. Mutually exclusive with `source_file`.",
							Optional:            true,
						},
						"source_file": schema.StringAttribute{
							MarkdownDescription: "Path to an existing file to copy. Mutually exclusive with `content`.",
							Optional:            true,
						},
						"executable": schema.BoolAttribute{
							MarkdownDescription: "Whether the file should be executable (mode 0755). Defaults to `false`.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(false),
						},
					},
				},
			},
		},
	}
}

// hookEventBlockSchema returns the schema for a hook event type block.
func hookEventBlockSchema(description string) schema.ListNestedBlock {
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
					MarkdownDescription: "Hook actions to execute when the matcher matches.",
					NestedObject: schema.NestedBlockObject{
						Attributes: map[string]schema.Attribute{
							"type": schema.StringAttribute{
								MarkdownDescription: "Hook type: `command`, `prompt`, or `agent`.",
								Required:            true,
								Validators: []validator.String{
									stringvalidator.OneOf("command", "prompt", "agent"),
								},
							},
							"command": schema.StringAttribute{
								MarkdownDescription: "Shell command to execute, prompt text, or agent description.",
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
// Configure (no-op – plugin resource doesn't need provider data)
// --------------------------------------------------------------------------

func (r *PluginResource) Configure(_ context.Context, _ resource.ConfigureRequest, _ *resource.ConfigureResponse) {
}

// --------------------------------------------------------------------------
// Create
// --------------------------------------------------------------------------

func (r *PluginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PluginResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags := r.writePlugin(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "created plugin", map[string]interface{}{
		"name":       plan.Name.ValueString(),
		"plugin_dir": plan.PluginDir.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Read
// --------------------------------------------------------------------------

func (r *PluginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PluginResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pluginDir := state.PluginDir.ValueString()
	manifestPath := filepath.Join(pluginDir, ".claude-plugin", "plugin.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			tflog.Info(ctx, "plugin manifest not found on disk, removing from state", map[string]interface{}{
				"plugin_dir": pluginDir,
			})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("File Read Failed", fmt.Sprintf("Failed to read plugin manifest %q: %s", manifestPath, err))
		return
	}

	diskContent := string(data)
	diskHash := computeHash(diskContent)

	state.ManifestJSON = types.StringValue(diskContent)
	state.ContentHash = types.StringValue(diskHash)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// --------------------------------------------------------------------------
// Update
// --------------------------------------------------------------------------

func (r *PluginResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PluginResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags := r.writePlugin(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "updated plugin", map[string]interface{}{
		"name":       plan.Name.ValueString(),
		"plugin_dir": plan.PluginDir.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// --------------------------------------------------------------------------
// Delete
// --------------------------------------------------------------------------

func (r *PluginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PluginResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pluginDir := state.PluginDir.ValueString()

	if err := os.RemoveAll(pluginDir); err != nil && !os.IsNotExist(err) {
		resp.Diagnostics.AddError("Directory Delete Failed", fmt.Sprintf("Failed to delete plugin directory %q: %s", pluginDir, err))
		return
	}

	tflog.Info(ctx, "deleted plugin directory", map[string]interface{}{
		"name":       state.Name.ValueString(),
		"plugin_dir": pluginDir,
	})
}

// --------------------------------------------------------------------------
// Plugin generation
// --------------------------------------------------------------------------

// pluginManifest represents the .claude-plugin/plugin.json structure.
type pluginManifest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Author      *manifestAuthor `json:"author,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Repository  string          `json:"repository,omitempty"`
	License     string          `json:"license,omitempty"`
	Keywords    []string        `json:"keywords,omitempty"`
	Commands    []string        `json:"commands,omitempty"`
	Agents      []string        `json:"agents,omitempty"`
	Skills      []string        `json:"skills,omitempty"`
	Hooks       interface{}     `json:"hooks,omitempty"`
	McpServers  interface{}     `json:"mcpServers,omitempty"`
	LspServers  interface{}     `json:"lspServers,omitempty"`
}

type manifestAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// writePlugin generates the complete plugin directory structure and sets
// computed attributes on the model.
func (r *PluginResource) writePlugin(ctx context.Context, model *PluginResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	outputDir := model.OutputDir.ValueString()

	// Resolve to absolute path.
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		diags.AddError("Path Resolution Failed", fmt.Sprintf("Failed to resolve absolute path for %q: %s", outputDir, err))
		return diags
	}

	// Create the plugin directory structure.
	if err := os.MkdirAll(filepath.Join(absDir, ".claude-plugin"), 0o755); err != nil {
		diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create plugin directory: %s", err))
		return diags
	}

	// Build the manifest.
	manifest := pluginManifest{
		Name: model.Name.ValueString(),
	}

	if !model.Version.IsNull() && !model.Version.IsUnknown() {
		manifest.Version = model.Version.ValueString()
	}
	if !model.Description.IsNull() && !model.Description.IsUnknown() {
		manifest.Description = model.Description.ValueString()
	}
	if !model.Homepage.IsNull() && !model.Homepage.IsUnknown() {
		manifest.Homepage = model.Homepage.ValueString()
	}
	if !model.Repository.IsNull() && !model.Repository.IsUnknown() {
		manifest.Repository = model.Repository.ValueString()
	}
	if !model.License.IsNull() && !model.License.IsUnknown() {
		manifest.License = model.License.ValueString()
	}
	if !model.Keywords.IsNull() && !model.Keywords.IsUnknown() {
		var keywords []string
		d := model.Keywords.ElementsAs(ctx, &keywords, false)
		diags.Append(d...)
		if diags.HasError() {
			return diags
		}
		manifest.Keywords = keywords
	}

	// Author
	if len(model.Author) > 0 {
		a := model.Author[0]
		ma := &manifestAuthor{Name: a.Name.ValueString()}
		if !a.Email.IsNull() && !a.Email.IsUnknown() {
			ma.Email = a.Email.ValueString()
		}
		if !a.URL.IsNull() && !a.URL.IsUnknown() {
			ma.URL = a.URL.ValueString()
		}
		manifest.Author = ma
	}

	// Skills
	if len(model.Skills) > 0 {
		skillsDir := filepath.Join(absDir, "skills")
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create skills directory: %s", err))
			return diags
		}

		var skillPaths []string
		for _, s := range model.Skills {
			name := s.Name.ValueString()
			skillDir := filepath.Join(skillsDir, name)

			if !s.SourceDir.IsNull() && !s.SourceDir.IsUnknown() {
				// Copy the entire source directory.
				srcDir := s.SourceDir.ValueString()
				d := copyDirectory(srcDir, skillDir)
				diags.Append(d...)
				if diags.HasError() {
					return diags
				}
			} else if !s.Content.IsNull() && !s.Content.IsUnknown() {
				// Write SKILL.md inline.
				if err := os.MkdirAll(skillDir, 0o755); err != nil {
					diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create skill directory %q: %s", skillDir, err))
					return diags
				}
				if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(s.Content.ValueString()), 0o644); err != nil {
					diags.AddError("File Write Failed", fmt.Sprintf("Failed to write SKILL.md for %q: %s", name, err))
					return diags
				}
			} else {
				diags.AddError("Invalid Skill Configuration",
					fmt.Sprintf("Skill %q must have either source_dir or content set.", name))
				return diags
			}

			skillPaths = append(skillPaths, fmt.Sprintf("./skills/%s/", name))
		}
		manifest.Skills = skillPaths
	}

	// Agents
	if len(model.Agents) > 0 {
		agentsDir := filepath.Join(absDir, "agents")
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create agents directory: %s", err))
			return diags
		}

		var agentPaths []string
		for _, a := range model.Agents {
			name := a.Name.ValueString()
			destPath := filepath.Join(agentsDir, name+".md")

			if !a.SourceFile.IsNull() && !a.SourceFile.IsUnknown() {
				d := copyFile(a.SourceFile.ValueString(), destPath)
				diags.Append(d...)
				if diags.HasError() {
					return diags
				}
			} else if !a.Content.IsNull() && !a.Content.IsUnknown() {
				if err := os.WriteFile(destPath, []byte(a.Content.ValueString()), 0o644); err != nil {
					diags.AddError("File Write Failed", fmt.Sprintf("Failed to write agent file for %q: %s", name, err))
					return diags
				}
			} else {
				diags.AddError("Invalid Agent Configuration",
					fmt.Sprintf("Agent %q must have either source_file or content set.", name))
				return diags
			}

			agentPaths = append(agentPaths, fmt.Sprintf("./agents/%s.md", name))
		}
		manifest.Agents = agentPaths
	}

	// Commands
	if len(model.Commands) > 0 {
		commandsDir := filepath.Join(absDir, "commands")
		if err := os.MkdirAll(commandsDir, 0o755); err != nil {
			diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create commands directory: %s", err))
			return diags
		}

		var cmdPaths []string
		for _, c := range model.Commands {
			name := c.Name.ValueString()
			destPath := filepath.Join(commandsDir, name+".md")

			if !c.SourceFile.IsNull() && !c.SourceFile.IsUnknown() {
				d := copyFile(c.SourceFile.ValueString(), destPath)
				diags.Append(d...)
				if diags.HasError() {
					return diags
				}
			} else if !c.Content.IsNull() && !c.Content.IsUnknown() {
				if err := os.WriteFile(destPath, []byte(c.Content.ValueString()), 0o644); err != nil {
					diags.AddError("File Write Failed", fmt.Sprintf("Failed to write command file for %q: %s", name, err))
					return diags
				}
			} else {
				diags.AddError("Invalid Command Configuration",
					fmt.Sprintf("Command %q must have either source_file or content set.", name))
				return diags
			}

			cmdPaths = append(cmdPaths, fmt.Sprintf("./commands/%s.md", name))
		}
		manifest.Commands = cmdPaths
	}

	// Hooks
	if len(model.Hooks) > 0 {
		hooksDir := filepath.Join(absDir, "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create hooks directory: %s", err))
			return diags
		}

		hooksConfig := r.buildHooksJSON(model.Hooks[0])
		if len(hooksConfig) > 0 {
			hooksJSON, err := marshalDeterministic(map[string]interface{}{"hooks": hooksConfig})
			if err != nil {
				diags.AddError("JSON Marshal Failed", fmt.Sprintf("Failed to marshal hooks configuration: %s", err))
				return diags
			}
			if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), hooksJSON, 0o644); err != nil {
				diags.AddError("File Write Failed", fmt.Sprintf("Failed to write hooks.json: %s", err))
				return diags
			}
			manifest.Hooks = "./hooks/hooks.json"
		}
	}

	// MCP Servers
	if len(model.McpServers) > 0 {
		mcpConfig := r.buildMcpJSON(ctx, model.McpServers, &diags)
		if diags.HasError() {
			return diags
		}
		mcpJSON, err := marshalDeterministic(map[string]interface{}{"mcpServers": mcpConfig})
		if err != nil {
			diags.AddError("JSON Marshal Failed", fmt.Sprintf("Failed to marshal MCP configuration: %s", err))
			return diags
		}
		if err := os.WriteFile(filepath.Join(absDir, ".mcp.json"), mcpJSON, 0o644); err != nil {
			diags.AddError("File Write Failed", fmt.Sprintf("Failed to write .mcp.json: %s", err))
			return diags
		}
		manifest.McpServers = "./.mcp.json"
	}

	// LSP Servers
	if len(model.LspServers) > 0 {
		lspConfig := r.buildLspJSON(ctx, model.LspServers, &diags)
		if diags.HasError() {
			return diags
		}
		lspJSON, err := marshalDeterministic(lspConfig)
		if err != nil {
			diags.AddError("JSON Marshal Failed", fmt.Sprintf("Failed to marshal LSP configuration: %s", err))
			return diags
		}
		if err := os.WriteFile(filepath.Join(absDir, ".lsp.json"), lspJSON, 0o644); err != nil {
			diags.AddError("File Write Failed", fmt.Sprintf("Failed to write .lsp.json: %s", err))
			return diags
		}
		manifest.LspServers = "./.lsp.json"
	}

	// Extra files
	for _, f := range model.Files {
		relPath := f.Path.ValueString()

		// Validate the path is relative and doesn't escape.
		if filepath.IsAbs(relPath) || strings.Contains(relPath, "..") {
			diags.AddError("Invalid File Path",
				fmt.Sprintf("File path %q must be relative and not contain '..'.", relPath))
			return diags
		}

		destPath := filepath.Join(absDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create parent directory for %q: %s", relPath, err))
			return diags
		}

		perm := os.FileMode(0o644)
		if f.Executable.ValueBool() {
			perm = 0o755
		}

		if !f.SourceFile.IsNull() && !f.SourceFile.IsUnknown() {
			data, err := os.ReadFile(f.SourceFile.ValueString())
			if err != nil {
				diags.AddError("File Read Failed", fmt.Sprintf("Failed to read source file %q: %s", f.SourceFile.ValueString(), err))
				return diags
			}
			if err := os.WriteFile(destPath, data, perm); err != nil {
				diags.AddError("File Write Failed", fmt.Sprintf("Failed to write file %q: %s", relPath, err))
				return diags
			}
		} else if !f.Content.IsNull() && !f.Content.IsUnknown() {
			if err := os.WriteFile(destPath, []byte(f.Content.ValueString()), perm); err != nil {
				diags.AddError("File Write Failed", fmt.Sprintf("Failed to write file %q: %s", relPath, err))
				return diags
			}
		} else {
			diags.AddError("Invalid File Configuration",
				fmt.Sprintf("File %q must have either content or source_file set.", relPath))
			return diags
		}
	}

	// Write the manifest.
	manifestJSON, err := marshalDeterministic(manifest)
	if err != nil {
		diags.AddError("JSON Marshal Failed", fmt.Sprintf("Failed to marshal plugin manifest: %s", err))
		return diags
	}

	manifestPath := filepath.Join(absDir, ".claude-plugin", "plugin.json")
	if err := os.WriteFile(manifestPath, manifestJSON, 0o644); err != nil {
		diags.AddError("File Write Failed", fmt.Sprintf("Failed to write plugin.json: %s", err))
		return diags
	}

	manifestStr := string(manifestJSON)
	hash := computeHash(manifestStr)

	model.ID = types.StringValue(absDir)
	model.PluginDir = types.StringValue(absDir)
	model.ManifestJSON = types.StringValue(manifestStr)
	model.ContentHash = types.StringValue(hash)

	return diags
}

// --------------------------------------------------------------------------
// Hook / MCP / LSP builders
// --------------------------------------------------------------------------

// buildHooksJSON converts the PluginHooksModel into a map suitable for JSON
// serialization matching the Claude Code hooks.json format.
func (r *PluginResource) buildHooksJSON(hooks PluginHooksModel) map[string]interface{} {
	result := make(map[string]interface{})

	addEvent := func(name string, matchers []PluginHookMatcherModel) {
		if len(matchers) == 0 {
			return
		}
		var entries []map[string]interface{}
		for _, m := range matchers {
			entry := make(map[string]interface{})
			if !m.Matcher.IsNull() && !m.Matcher.IsUnknown() {
				entry["matcher"] = m.Matcher.ValueString()
			}
			var hookList []map[string]interface{}
			for _, h := range m.Hooks {
				hookList = append(hookList, map[string]interface{}{
					"type":    h.Type.ValueString(),
					"command": h.Command.ValueString(),
				})
			}
			entry["hooks"] = hookList
			entries = append(entries, entry)
		}
		result[name] = entries
	}

	addEvent("PreToolUse", hooks.PreToolUse)
	addEvent("PostToolUse", hooks.PostToolUse)
	addEvent("PostToolUseFailure", hooks.PostToolUseFail)
	addEvent("UserPromptSubmit", hooks.UserPromptSubmit)
	addEvent("Notification", hooks.Notification)
	addEvent("Stop", hooks.Stop)
	addEvent("SubagentStart", hooks.SubagentStart)
	addEvent("SubagentStop", hooks.SubagentStop)
	addEvent("SessionStart", hooks.SessionStart)
	addEvent("SessionEnd", hooks.SessionEnd)
	addEvent("PreCompact", hooks.PreCompact)

	return result
}

// buildMcpJSON converts PluginMcpModel entries into an ordered map for .mcp.json.
func (r *PluginResource) buildMcpJSON(ctx context.Context, servers []PluginMcpModel, diags *diag.Diagnostics) map[string]interface{} {
	result := make(map[string]interface{})
	for _, s := range servers {
		entry := make(map[string]interface{})

		if !s.Command.IsNull() && !s.Command.IsUnknown() {
			entry["command"] = s.Command.ValueString()
		}
		if !s.URL.IsNull() && !s.URL.IsUnknown() {
			entry["url"] = s.URL.ValueString()
		}
		if !s.Cwd.IsNull() && !s.Cwd.IsUnknown() {
			entry["cwd"] = s.Cwd.ValueString()
		}
		if !s.Args.IsNull() && !s.Args.IsUnknown() {
			var args []string
			d := s.Args.ElementsAs(ctx, &args, false)
			diags.Append(d...)
			if diags.HasError() {
				return nil
			}
			entry["args"] = args
		}
		if !s.Env.IsNull() && !s.Env.IsUnknown() {
			env := make(map[string]string)
			d := s.Env.ElementsAs(ctx, &env, false)
			diags.Append(d...)
			if diags.HasError() {
				return nil
			}
			entry["env"] = env
		}

		result[s.Name.ValueString()] = entry
	}
	return result
}

// buildLspJSON converts PluginLspModel entries into a map for .lsp.json.
func (r *PluginResource) buildLspJSON(ctx context.Context, servers []PluginLspModel, diags *diag.Diagnostics) map[string]interface{} {
	result := make(map[string]interface{})
	for _, s := range servers {
		entry := make(map[string]interface{})

		entry["command"] = s.Command.ValueString()

		if !s.Args.IsNull() && !s.Args.IsUnknown() {
			var args []string
			d := s.Args.ElementsAs(ctx, &args, false)
			diags.Append(d...)
			if diags.HasError() {
				return nil
			}
			entry["args"] = args
		}
		if !s.Transport.IsNull() && !s.Transport.IsUnknown() {
			entry["transport"] = s.Transport.ValueString()
		}
		if !s.Env.IsNull() && !s.Env.IsUnknown() {
			env := make(map[string]string)
			d := s.Env.ElementsAs(ctx, &env, false)
			diags.Append(d...)
			if diags.HasError() {
				return nil
			}
			entry["env"] = env
		}
		if !s.InitializationOptions.IsNull() && !s.InitializationOptions.IsUnknown() {
			opts := make(map[string]string)
			d := s.InitializationOptions.ElementsAs(ctx, &opts, false)
			diags.Append(d...)
			if diags.HasError() {
				return nil
			}
			entry["initializationOptions"] = opts
		}
		if !s.Settings.IsNull() && !s.Settings.IsUnknown() {
			settings := make(map[string]string)
			d := s.Settings.ElementsAs(ctx, &settings, false)
			diags.Append(d...)
			if diags.HasError() {
				return nil
			}
			entry["settings"] = settings
		}

		// extension_to_language is required
		extMap := make(map[string]string)
		d := s.ExtensionToLanguage.ElementsAs(ctx, &extMap, false)
		diags.Append(d...)
		if diags.HasError() {
			return nil
		}
		entry["extensionToLanguage"] = extMap

		if s.RestartOnCrash.ValueBool() {
			entry["restartOnCrash"] = true
		}
		if !s.MaxRestarts.IsNull() && !s.MaxRestarts.IsUnknown() {
			entry["maxRestarts"] = s.MaxRestarts.ValueInt64()
		}

		result[s.Name.ValueString()] = entry
	}
	return result
}

// --------------------------------------------------------------------------
// File operation helpers
// --------------------------------------------------------------------------

// copyDirectory recursively copies a source directory to a destination.
func copyDirectory(src, dst string) diag.Diagnostics {
	var diags diag.Diagnostics

	src, err := filepath.Abs(src)
	if err != nil {
		diags.AddError("Path Resolution Failed", fmt.Sprintf("Failed to resolve source path %q: %s", src, err))
		return diags
	}

	// Ensure the source exists and is a directory.
	info, err := os.Stat(src)
	if err != nil {
		diags.AddError("Source Directory Error", fmt.Sprintf("Failed to stat source directory %q: %s", src, err))
		return diags
	}
	if !info.IsDir() {
		diags.AddError("Source Not a Directory", fmt.Sprintf("Source path %q is not a directory.", src))
		return diags
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create destination directory %q: %s", dst, err))
		return diags
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		diags.AddError("Directory Read Failed", fmt.Sprintf("Failed to read source directory %q: %s", src, err))
		return diags
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			d := copyDirectory(srcPath, dstPath)
			diags.Append(d...)
			if diags.HasError() {
				return diags
			}
		} else {
			d := copyFile(srcPath, dstPath)
			diags.Append(d...)
			if diags.HasError() {
				return diags
			}
		}
	}

	return diags
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) diag.Diagnostics {
	var diags diag.Diagnostics

	srcFile, err := os.Open(src)
	if err != nil {
		diags.AddError("File Read Failed", fmt.Sprintf("Failed to open source file %q: %s", src, err))
		return diags
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		diags.AddError("File Stat Failed", fmt.Sprintf("Failed to stat source file %q: %s", src, err))
		return diags
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		diags.AddError("Directory Create Failed", fmt.Sprintf("Failed to create parent directory for %q: %s", dst, err))
		return diags
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		diags.AddError("File Write Failed", fmt.Sprintf("Failed to create destination file %q: %s", dst, err))
		return diags
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		diags.AddError("File Copy Failed", fmt.Sprintf("Failed to copy %q to %q: %s", src, dst, err))
		return diags
	}

	return diags
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

// marshalDeterministic marshals a value to indented JSON with sorted map keys
// and a trailing newline. Go's encoding/json sorts map[string]* keys
// lexicographically by default, and struct fields are serialized in
// declaration order, so the output is deterministic.
func marshalDeterministic(v interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
