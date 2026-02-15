package plugin

import "github.com/hashicorp/terraform-plugin-framework/types"

// PluginResourceModel maps the agentctx_plugin resource schema to a Go struct.
type PluginResourceModel struct {
	// Required
	Name      types.String `tfsdk:"name"`
	OutputDir types.String `tfsdk:"output_dir"`

	// Optional – metadata
	Version     types.String `tfsdk:"version"`
	Description types.String `tfsdk:"description"`
	Homepage    types.String `tfsdk:"homepage"`
	Repository  types.String `tfsdk:"repository"`
	License     types.String `tfsdk:"license"`
	Keywords    types.List   `tfsdk:"keywords"`

	// Optional – author block
	Author []AuthorModel `tfsdk:"author"`

	// Optional – component blocks
	OutputStyles []PluginOutputStyleModel `tfsdk:"output_style"`
	Skills       []PluginSkillModel       `tfsdk:"skill"`
	Agents       []PluginAgentModel       `tfsdk:"agent"`
	Commands     []PluginCommandModel     `tfsdk:"command"`
	McpServers   []PluginMcpModel         `tfsdk:"mcp_server"`
	LspServers   []PluginLspModel         `tfsdk:"lsp_server"`
	Hooks        []PluginHooksModel       `tfsdk:"hooks"`
	Files        []PluginFileModel        `tfsdk:"file"`

	// Computed
	ID           types.String `tfsdk:"id"`
	PluginDir    types.String `tfsdk:"plugin_dir"`
	ManifestJSON types.String `tfsdk:"manifest_json"`
	ContentHash  types.String `tfsdk:"content_hash"`
}

// AuthorModel maps the author {} block.
type AuthorModel struct {
	Name  types.String `tfsdk:"name"`
	Email types.String `tfsdk:"email"`
	URL   types.String `tfsdk:"url"`
}

// PluginOutputStyleModel maps an output_style {} block.
type PluginOutputStyleModel struct {
	Path types.String `tfsdk:"path"`
}

// PluginSkillModel maps a skill {} block. Skills can be sourced from a local
// directory (source_dir) or defined inline (content). When source_dir is set,
// the entire directory is copied into skills/<name>/. When content is set,
// a SKILL.md file is written to skills/<name>/SKILL.md.
type PluginSkillModel struct {
	Name      types.String `tfsdk:"name"`
	SourceDir types.String `tfsdk:"source_dir"`
	Content   types.String `tfsdk:"content"`
}

// PluginAgentModel maps an agent {} block. Agents can be sourced from an
// existing file (source_file) or defined inline (content).
type PluginAgentModel struct {
	Name       types.String `tfsdk:"name"`
	SourceFile types.String `tfsdk:"source_file"`
	Content    types.String `tfsdk:"content"`
}

// PluginCommandModel maps a command {} block.
type PluginCommandModel struct {
	Name       types.String `tfsdk:"name"`
	SourceFile types.String `tfsdk:"source_file"`
	Content    types.String `tfsdk:"content"`
}

// PluginMcpModel maps an mcp_server {} block for the plugin's .mcp.json.
type PluginMcpModel struct {
	Name    types.String `tfsdk:"name"`
	Command types.String `tfsdk:"command"`
	Args    types.List   `tfsdk:"args"`
	Env     types.Map    `tfsdk:"env"`
	URL     types.String `tfsdk:"url"`
	Cwd     types.String `tfsdk:"cwd"`
}

// PluginLspModel maps an lsp_server {} block for the plugin's .lsp.json.
type PluginLspModel struct {
	Name                  types.String `tfsdk:"name"`
	Command               types.String `tfsdk:"command"`
	Args                  types.List   `tfsdk:"args"`
	Transport             types.String `tfsdk:"transport"`
	Env                   types.Map    `tfsdk:"env"`
	InitializationOptions types.Map    `tfsdk:"initialization_options"`
	Settings              types.Map    `tfsdk:"settings"`
	ExtensionToLanguage   types.Map    `tfsdk:"extension_to_language"`
	WorkspaceFolder       types.String `tfsdk:"workspace_folder"`
	StartupTimeout        types.Int64  `tfsdk:"startup_timeout"`
	ShutdownTimeout       types.Int64  `tfsdk:"shutdown_timeout"`
	RestartOnCrash        types.Bool   `tfsdk:"restart_on_crash"`
	MaxRestarts           types.Int64  `tfsdk:"max_restarts"`
}

// PluginHooksModel maps the hooks {} block.
type PluginHooksModel struct {
	PreToolUse        []PluginHookMatcherModel `tfsdk:"pre_tool_use"`
	PostToolUse       []PluginHookMatcherModel `tfsdk:"post_tool_use"`
	PostToolUseFail   []PluginHookMatcherModel `tfsdk:"post_tool_use_failure"`
	PermissionRequest []PluginHookMatcherModel `tfsdk:"permission_request"`
	UserPromptSubmit  []PluginHookMatcherModel `tfsdk:"user_prompt_submit"`
	Notification      []PluginHookMatcherModel `tfsdk:"notification"`
	Stop              []PluginHookMatcherModel `tfsdk:"stop"`
	SubagentStart     []PluginHookMatcherModel `tfsdk:"subagent_start"`
	SubagentStop      []PluginHookMatcherModel `tfsdk:"subagent_stop"`
	SessionStart      []PluginHookMatcherModel `tfsdk:"session_start"`
	SessionEnd        []PluginHookMatcherModel `tfsdk:"session_end"`
	TeammateIdle      []PluginHookMatcherModel `tfsdk:"teammate_idle"`
	TaskCompleted     []PluginHookMatcherModel `tfsdk:"task_completed"`
	PreCompact        []PluginHookMatcherModel `tfsdk:"pre_compact"`
}

// PluginHookMatcherModel maps a single hook matcher entry.
type PluginHookMatcherModel struct {
	Matcher types.String           `tfsdk:"matcher"`
	Hooks   []PluginHookEntryModel `tfsdk:"hook"`
}

// PluginHookEntryModel maps a single hook command entry.
type PluginHookEntryModel struct {
	Type    types.String `tfsdk:"type"`
	Command types.String `tfsdk:"command"`
}

// PluginFileModel maps a file {} block for bundling extra files into the plugin.
type PluginFileModel struct {
	Path       types.String `tfsdk:"path"`
	Content    types.String `tfsdk:"content"`
	SourceFile types.String `tfsdk:"source_file"`
	Executable types.Bool   `tfsdk:"executable"`
}
