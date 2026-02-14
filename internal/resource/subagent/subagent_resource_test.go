package subagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestComputeHash(t *testing.T) {
	content := "hello world"
	hash := computeHash(content)

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected hash to start with 'sha256:', got %q", hash)
	}

	// SHA-256 hex digest is 64 characters.
	hexPart := strings.TrimPrefix(hash, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("expected 64 hex characters after prefix, got %d", len(hexPart))
	}

	// Same input should produce the same hash.
	hash2 := computeHash(content)
	if hash != hash2 {
		t.Errorf("expected deterministic hashing, got %q and %q", hash, hash2)
	}

	// Different input should produce a different hash.
	hash3 := computeHash("different content")
	if hash == hash3 {
		t.Errorf("expected different hashes for different content")
	}
}

func TestConvertHookMatchers(t *testing.T) {
	tests := []struct {
		name     string
		matchers []HookMatcherModel
		wantLen  int
	}{
		{
			name:     "empty",
			matchers: nil,
			wantLen:  0,
		},
		{
			name: "single matcher with hooks",
			matchers: []HookMatcherModel{
				{
					Matcher: stringValue("Bash"),
					Hooks: []HookEntryModel{
						{Type: stringValue("command"), Command: stringValue("./validate.sh")},
					},
				},
			},
			wantLen: 1,
		},
		{
			name: "multiple matchers",
			matchers: []HookMatcherModel{
				{
					Matcher: stringValue("Bash"),
					Hooks: []HookEntryModel{
						{Type: stringValue("command"), Command: stringValue("./validate.sh")},
					},
				},
				{
					Hooks: []HookEntryModel{
						{Type: stringValue("command"), Command: stringValue("./cleanup.sh")},
					},
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHookMatchers(tt.matchers)
			if len(result) != tt.wantLen {
				t.Errorf("expected %d matchers, got %d", tt.wantLen, len(result))
			}
		})
	}
}

func TestConvertHookMatchers_Content(t *testing.T) {
	matchers := []HookMatcherModel{
		{
			Matcher: stringValue("Bash"),
			Hooks: []HookEntryModel{
				{Type: stringValue("command"), Command: stringValue("./validate.sh")},
				{Type: stringValue("command"), Command: stringValue("./log.sh")},
			},
		},
	}

	result := convertHookMatchers(matchers)
	if len(result) != 1 {
		t.Fatalf("expected 1 matcher, got %d", len(result))
	}

	if result[0].Matcher != "Bash" {
		t.Errorf("expected matcher 'Bash', got %q", result[0].Matcher)
	}
	if len(result[0].Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(result[0].Hooks))
	}
	if result[0].Hooks[0].Command != "./validate.sh" {
		t.Errorf("expected first hook command './validate.sh', got %q", result[0].Hooks[0].Command)
	}
	if result[0].Hooks[1].Command != "./log.sh" {
		t.Errorf("expected second hook command './log.sh', got %q", result[0].Hooks[1].Command)
	}
}

// --------------------------------------------------------------------------
// renderContent tests
// --------------------------------------------------------------------------

func TestRenderContent_BasicRequired(t *testing.T) {
	r := &SubagentResource{}
	model := &SubagentResourceModel{
		Name:        stringValue("code-reviewer"),
		Description: stringValue("Reviews code for quality"),
		Prompt:      stringValue("You are a code reviewer."),
		// All optional fields left as null
		Tools:           types.ListNull(types.StringType),
		DisallowedTools: types.ListNull(types.StringType),
		Skills:          types.ListNull(types.StringType),
	}

	content, diags := r.renderContent(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected error: %s", diags.Errors())
	}

	// Should start with YAML frontmatter delimiters
	if !strings.HasPrefix(content, "---\n") {
		t.Error("content should start with '---'")
	}

	// Should contain required fields
	assertContains(t, content, "name: code-reviewer")
	assertContains(t, content, "description: Reviews code for quality")

	// Should end with the prompt
	assertContains(t, content, "You are a code reviewer.\n")

	// Should have frontmatter closing delimiter before the prompt
	assertContains(t, content, "---\n\nYou are a code reviewer.")

	// Should NOT contain optional fields when unset
	assertNotContains(t, content, "model:")
	assertNotContains(t, content, "tools:")
	assertNotContains(t, content, "disallowedTools:")
	assertNotContains(t, content, "permissionMode:")
	assertNotContains(t, content, "maxTurns:")
	assertNotContains(t, content, "skills:")
	assertNotContains(t, content, "memory:")
	assertNotContains(t, content, "mcpServers:")
	assertNotContains(t, content, "hooks:")
}

func TestRenderContent_AllSimpleFields(t *testing.T) {
	r := &SubagentResource{}

	tools, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"Read", "Grep", "Bash"})
	disallowed, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"Write", "Edit"})
	skills, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"api-conventions"})

	model := &SubagentResourceModel{
		Name:            stringValue("full-agent"),
		Description:     stringValue("A fully configured agent"),
		Prompt:          stringValue("You are a specialized agent."),
		Model:           stringValue("sonnet"),
		Tools:           tools,
		DisallowedTools: disallowed,
		PermissionMode:  stringValue("acceptEdits"),
		MaxTurns:        types.Int64Value(50),
		Skills:          skills,
		Memory:          stringValue("user"),
	}

	content, diags := r.renderContent(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected error: %s", diags.Errors())
	}

	assertContains(t, content, "name: full-agent")
	assertContains(t, content, "model: sonnet")
	assertContains(t, content, "tools: Read, Grep, Bash")
	assertContains(t, content, "disallowedTools: Write, Edit")
	assertContains(t, content, "permissionMode: acceptEdits")
	assertContains(t, content, "maxTurns: 50")
	assertContains(t, content, "memory: user")
	assertContains(t, content, "- api-conventions")
	assertContains(t, content, "You are a specialized agent.\n")
}

func TestRenderContent_TaskToolSyntax(t *testing.T) {
	r := &SubagentResource{}

	tools, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"Task(worker, researcher)", "Read", "Bash"})

	model := &SubagentResourceModel{
		Name:            stringValue("coordinator"),
		Description:     stringValue("Coordinates work"),
		Prompt:          stringValue("You are a coordinator."),
		Tools:           tools,
		DisallowedTools: types.ListNull(types.StringType),
		Skills:          types.ListNull(types.StringType),
	}

	content, diags := r.renderContent(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected error: %s", diags.Errors())
	}

	// Task(agent_type) syntax should be preserved in the comma-separated tools string
	assertContains(t, content, "tools: Task(worker, researcher), Read, Bash")
}

func TestRenderContent_WithMcpServers(t *testing.T) {
	r := &SubagentResource{}

	model := &SubagentResourceModel{
		Name:        stringValue("mcp-agent"),
		Description: stringValue("Agent with MCP"),
		Prompt:      stringValue("You are an agent."),
		Tools:       types.ListNull(types.StringType),
		DisallowedTools: types.ListNull(types.StringType),
		Skills:      types.ListNull(types.StringType),
		McpServers: []McpServerModel{
			{
				Name:    stringValue("slack"),
				Command: types.StringNull(),
				Args:    types.ListNull(types.StringType),
				Env:     types.MapNull(types.StringType),
				URL:     types.StringNull(),
			},
			{
				Name:    stringValue("custom"),
				Command: stringValue("node"),
				Args:    listValue("server.js"),
				Env:     types.MapNull(types.StringType),
				URL:     types.StringNull(),
			},
		},
	}

	content, diags := r.renderContent(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected error: %s", diags.Errors())
	}

	assertContains(t, content, "mcpServers:")
	assertContains(t, content, "slack:")
	assertContains(t, content, "custom:")
	assertContains(t, content, "command: node")
	assertContains(t, content, "- server.js")
}

func TestRenderContent_WithHooks(t *testing.T) {
	r := &SubagentResource{}

	model := &SubagentResourceModel{
		Name:            stringValue("hooked-agent"),
		Description:     stringValue("Agent with hooks"),
		Prompt:          stringValue("You are an agent."),
		Tools:           types.ListNull(types.StringType),
		DisallowedTools: types.ListNull(types.StringType),
		Skills:          types.ListNull(types.StringType),
		Hooks: []HooksModel{
			{
				PreToolUse: []HookMatcherModel{
					{
						Matcher: stringValue("Bash"),
						Hooks: []HookEntryModel{
							{Type: stringValue("command"), Command: stringValue("./validate.sh")},
						},
					},
				},
				PostToolUse: []HookMatcherModel{
					{
						Matcher: stringValue("Edit|Write"),
						Hooks: []HookEntryModel{
							{Type: stringValue("command"), Command: stringValue("./lint.sh")},
						},
					},
				},
				Stop: []HookMatcherModel{
					{
						Hooks: []HookEntryModel{
							{Type: stringValue("command"), Command: stringValue("./cleanup.sh")},
						},
					},
				},
			},
		},
	}

	content, diags := r.renderContent(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected error: %s", diags.Errors())
	}

	assertContains(t, content, "hooks:")
	assertContains(t, content, "PreToolUse:")
	assertContains(t, content, "PostToolUse:")
	assertContains(t, content, "Stop:")
	assertContains(t, content, "matcher: Bash")
	assertContains(t, content, "matcher: Edit|Write")
	assertContains(t, content, "./validate.sh")
	assertContains(t, content, "./lint.sh")
	assertContains(t, content, "./cleanup.sh")
}

func TestRenderContent_PromptWhitespaceTrimmed(t *testing.T) {
	r := &SubagentResource{}
	model := &SubagentResourceModel{
		Name:            stringValue("trimmed"),
		Description:     stringValue("test"),
		Prompt:          stringValue("  \n  Some prompt.\n\n  "),
		Tools:           types.ListNull(types.StringType),
		DisallowedTools: types.ListNull(types.StringType),
		Skills:          types.ListNull(types.StringType),
	}

	content, diags := r.renderContent(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected error: %s", diags.Errors())
	}

	// Prompt should be trimmed and end with exactly one newline
	if !strings.HasSuffix(content, "Some prompt.\n") {
		t.Errorf("expected content to end with trimmed prompt + newline, got: %q", content[len(content)-30:])
	}
}

// --------------------------------------------------------------------------
// writeFile tests
// --------------------------------------------------------------------------

func TestWriteFile_CreatesDirectoryAndFile(t *testing.T) {
	r := &SubagentResource{}
	dir := filepath.Join(t.TempDir(), "nested", "agents")

	model := &SubagentResourceModel{
		Name:      stringValue("test-agent"),
		OutputDir: stringValue(dir),
	}

	filePath, err := r.writeFile(context.Background(), model, "test content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have created the nested directory
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected output directory to be created")
	}

	// Should return an absolute path
	if !filepath.IsAbs(filePath) {
		t.Errorf("expected absolute path, got %q", filePath)
	}

	// File should exist with correct content
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("expected 'test content', got %q", string(data))
	}

	// Filename should be name + .md
	if filepath.Base(filePath) != "test-agent.md" {
		t.Errorf("expected filename 'test-agent.md', got %q", filepath.Base(filePath))
	}
}

func TestWriteFile_OverwritesExisting(t *testing.T) {
	r := &SubagentResource{}
	dir := t.TempDir()

	model := &SubagentResourceModel{
		Name:      stringValue("overwrite-test"),
		OutputDir: stringValue(dir),
	}

	_, err := r.writeFile(context.Background(), model, "original")
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	filePath, err := r.writeFile(context.Background(), model, "updated")
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "updated" {
		t.Errorf("expected 'updated', got %q", string(data))
	}
}

// --------------------------------------------------------------------------
// Name pattern validation tests
// --------------------------------------------------------------------------

func TestNamePattern(t *testing.T) {
	valid := []string{
		"code-reviewer",
		"agent",
		"my-agent-v2",
		"a",
		"a1",
		"test123",
		"db-reader",
	}
	invalid := []string{
		"",
		"-agent",
		"agent-",
		"Agent",
		"my_agent",
		"my agent",
		"agent--double",
		"UPPERCASE",
		"with.dot",
		"with/slash",
	}

	for _, name := range valid {
		if !namePattern.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
	for _, name := range invalid {
		if namePattern.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

// stringValue returns a types.String with the given value.
func stringValue(s string) types.String {
	return types.StringValue(s)
}

// listValue returns a types.List of strings from the given values.
func listValue(vals ...string) types.List {
	l, _ := types.ListValueFrom(context.Background(), types.StringType, vals)
	return l
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("expected content to contain %q, got:\n%s", substr, content)
	}
}

func assertNotContains(t *testing.T, content, substr string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("expected content NOT to contain %q, got:\n%s", substr, content)
	}
}
