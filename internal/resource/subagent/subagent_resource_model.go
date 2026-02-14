package subagent

import "github.com/hashicorp/terraform-plugin-framework/types"

// SubagentResourceModel maps the agentctx_subagent resource schema to a Go
// struct. It captures every supported Claude Code sub-agent frontmatter field
// plus the output directory and rendered content.
type SubagentResourceModel struct {
	// Required
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	OutputDir   types.String `tfsdk:"output_dir"`
	Prompt      types.String `tfsdk:"prompt"`

	// Optional – simple fields
	Model           types.String `tfsdk:"model"`
	Tools           types.List   `tfsdk:"tools"`
	DisallowedTools types.List   `tfsdk:"disallowed_tools"`
	PermissionMode  types.String `tfsdk:"permission_mode"`
	MaxTurns        types.Int64  `tfsdk:"max_turns"`
	Skills          types.List   `tfsdk:"skills"`
	Memory          types.String `tfsdk:"memory"`

	// Optional – blocks
	McpServers []McpServerModel `tfsdk:"mcp_server"`
	Hooks      []HooksModel     `tfsdk:"hooks"`

	// Computed
	ID          types.String `tfsdk:"id"`
	Content     types.String `tfsdk:"content"`
	FilePath    types.String `tfsdk:"file_path"`
	ContentHash types.String `tfsdk:"content_hash"`
}

// HooksModel maps the hooks {} block.
type HooksModel struct {
	PreToolUse  []HookMatcherModel `tfsdk:"pre_tool_use"`
	PostToolUse []HookMatcherModel `tfsdk:"post_tool_use"`
	Stop        []HookMatcherModel `tfsdk:"stop"`
}

// HookMatcherModel maps a single hook matcher entry within a hook event type.
type HookMatcherModel struct {
	Matcher types.String     `tfsdk:"matcher"`
	Hooks   []HookEntryModel `tfsdk:"hook"`
}

// HookEntryModel maps a single hook command entry.
type HookEntryModel struct {
	Type    types.String `tfsdk:"type"`
	Command types.String `tfsdk:"command"`
}

// McpServerModel maps a single mcp_server {} block.
type McpServerModel struct {
	Name    types.String `tfsdk:"name"`
	Command types.String `tfsdk:"command"`
	Args    types.List   `tfsdk:"args"`
	Env     types.Map    `tfsdk:"env"`
	URL     types.String `tfsdk:"url"`
}
