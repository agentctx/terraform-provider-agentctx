package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --------------------------------------------------------------------------
// computeHash tests
// --------------------------------------------------------------------------

func TestComputeHash(t *testing.T) {
	content := "hello world"
	hash := computeHash(content)

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected hash to start with 'sha256:', got %q", hash)
	}

	hexPart := strings.TrimPrefix(hash, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("expected 64 hex characters after prefix, got %d", len(hexPart))
	}

	// Deterministic
	if hash != computeHash(content) {
		t.Error("expected deterministic hashing")
	}

	// Different input → different hash
	if hash == computeHash("different content") {
		t.Error("expected different hashes for different content")
	}
}

// --------------------------------------------------------------------------
// namePattern tests
// --------------------------------------------------------------------------

func TestNamePattern(t *testing.T) {
	valid := []string{"my-plugin", "plugin", "a", "plugin-v2", "a1", "test123"}
	invalid := []string{"", "-plugin", "plugin-", "Plugin", "my_plugin", "my plugin", "UPPER", "with.dot"}

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
// marshalDeterministic tests
// --------------------------------------------------------------------------

func TestMarshalDeterministic(t *testing.T) {
	data := map[string]interface{}{
		"b": "second",
		"a": "first",
	}

	result, err := marshalDeterministic(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Keys should be sorted
	out := string(result)
	aIdx := strings.Index(out, `"a"`)
	bIdx := strings.Index(out, `"b"`)
	if aIdx >= bIdx {
		t.Errorf("expected key 'a' before 'b' in output: %s", out)
	}

	// Should end with newline
	if !strings.HasSuffix(out, "\n") {
		t.Error("expected trailing newline")
	}

	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Errorf("expected valid JSON: %v", err)
	}
}

// --------------------------------------------------------------------------
// writePlugin tests
// --------------------------------------------------------------------------

func TestWritePlugin_BasicManifest(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "test-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("test-plugin"),
		OutputDir: stringValue(dir),
		Version:   stringValue("1.0.0"),
		Description: stringValue("A test plugin"),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify plugin.json was created
	manifestPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON manifest: %v", err)
	}

	if manifest["name"] != "test-plugin" {
		t.Errorf("expected name 'test-plugin', got %v", manifest["name"])
	}
	if manifest["version"] != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %v", manifest["version"])
	}
	if manifest["description"] != "A test plugin" {
		t.Errorf("expected description 'A test plugin', got %v", manifest["description"])
	}

	// Computed fields should be set
	if model.ID.IsNull() || model.ID.IsUnknown() {
		t.Error("expected ID to be set")
	}
	if model.PluginDir.IsNull() || model.PluginDir.IsUnknown() {
		t.Error("expected PluginDir to be set")
	}
	if model.ManifestJSON.IsNull() || model.ManifestJSON.IsUnknown() {
		t.Error("expected ManifestJSON to be set")
	}
	if model.ContentHash.IsNull() || model.ContentHash.IsUnknown() {
		t.Error("expected ContentHash to be set")
	}
	if !strings.HasPrefix(model.ContentHash.ValueString(), "sha256:") {
		t.Errorf("expected hash prefix, got %q", model.ContentHash.ValueString())
	}
}

func TestWritePlugin_WithAuthor(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "author-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("author-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Author: []AuthorModel{
			{
				Name:  stringValue("Test Author"),
				Email: stringValue("test@example.com"),
				URL:   stringValue("https://github.com/test"),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(model.ManifestJSON.ValueString()), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	author, ok := manifest["author"].(map[string]interface{})
	if !ok {
		t.Fatal("expected author object in manifest")
	}
	if author["name"] != "Test Author" {
		t.Errorf("expected author name 'Test Author', got %v", author["name"])
	}
	if author["email"] != "test@example.com" {
		t.Errorf("expected author email, got %v", author["email"])
	}
	if author["url"] != "https://github.com/test" {
		t.Errorf("expected author url, got %v", author["url"])
	}
}

func TestWritePlugin_WithInlineSkill(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "skill-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("skill-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Skills: []PluginSkillModel{
			{
				Name:      stringValue("code-reviewer"),
				SourceDir: types.StringNull(),
				Content:   stringValue("# Code Review Skill\n\nReview code for quality."),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify SKILL.md was created
	skillPath := filepath.Join(dir, "skills", "code-reviewer", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill file: %v", err)
	}
	if string(data) != "# Code Review Skill\n\nReview code for quality." {
		t.Errorf("unexpected skill content: %q", string(data))
	}

	// Verify manifest references the skill
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(model.ManifestJSON.ValueString()), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	skills, ok := manifest["skills"].([]interface{})
	if !ok || len(skills) != 1 {
		t.Fatalf("expected 1 skill in manifest, got %v", manifest["skills"])
	}
	if skills[0] != "./skills/code-reviewer/" {
		t.Errorf("expected skill path './skills/code-reviewer/', got %v", skills[0])
	}
}

func TestWritePlugin_WithSourceDirSkill(t *testing.T) {
	r := &PluginResource{}

	// Create a source skill directory
	srcDir := filepath.Join(t.TempDir(), "src-skill")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("# Copied Skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "reference.md"), []byte("# Reference"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "srcdir-plugin")
	model := &PluginResourceModel{
		Name:      stringValue("srcdir-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Skills: []PluginSkillModel{
			{
				Name:      stringValue("copied-skill"),
				SourceDir: stringValue(srcDir),
				Content:   types.StringNull(),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Both files should be copied
	skillDir := filepath.Join(dir, "skills", "copied-skill")
	assertFileContent(t, filepath.Join(skillDir, "SKILL.md"), "# Copied Skill")
	assertFileContent(t, filepath.Join(skillDir, "reference.md"), "# Reference")
}

func TestWritePlugin_WithInlineAgent(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "agent-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("agent-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Agents: []PluginAgentModel{
			{
				Name:       stringValue("security-reviewer"),
				SourceFile: types.StringNull(),
				Content:    stringValue("---\nname: security-reviewer\n---\n\nYou review code for security issues."),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	agentPath := filepath.Join(dir, "agents", "security-reviewer.md")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("failed to read agent file: %v", err)
	}
	if !strings.Contains(string(data), "security-reviewer") {
		t.Error("expected agent content to contain name")
	}

	// Verify manifest
	var manifest map[string]interface{}
	json.Unmarshal([]byte(model.ManifestJSON.ValueString()), &manifest)
	agents := manifest["agents"].([]interface{})
	if agents[0] != "./agents/security-reviewer.md" {
		t.Errorf("expected agent path, got %v", agents[0])
	}
}

func TestWritePlugin_WithSourceFileAgent(t *testing.T) {
	r := &PluginResource{}

	// Create a source agent file (simulating agentctx_subagent output)
	srcFile := filepath.Join(t.TempDir(), "agents", "test-agent.md")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("---\nname: test-agent\n---\n\nAgent prompt."), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "srcfile-plugin")
	model := &PluginResourceModel{
		Name:      stringValue("srcfile-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Agents: []PluginAgentModel{
			{
				Name:       stringValue("test-agent"),
				SourceFile: stringValue(srcFile),
				Content:    types.StringNull(),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	assertFileContent(t, filepath.Join(dir, "agents", "test-agent.md"), "---\nname: test-agent\n---\n\nAgent prompt.")
}

func TestWritePlugin_WithCommand(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "cmd-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("cmd-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Commands: []PluginCommandModel{
			{
				Name:       stringValue("deploy"),
				SourceFile: types.StringNull(),
				Content:    stringValue("Deploy the application to production."),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	assertFileContent(t, filepath.Join(dir, "commands", "deploy.md"), "Deploy the application to production.")
}

func TestWritePlugin_WithHooks(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "hooks-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("hooks-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Hooks: []PluginHooksModel{
			{
				PostToolUse: []PluginHookMatcherModel{
					{
						Matcher: stringValue("Write|Edit"),
						Hooks: []PluginHookEntryModel{
							{Type: stringValue("command"), Command: stringValue("${CLAUDE_PLUGIN_ROOT}/scripts/format.sh")},
						},
					},
				},
				Stop: []PluginHookMatcherModel{
					{
						Matcher: types.StringNull(),
						Hooks: []PluginHookEntryModel{
							{Type: stringValue("command"), Command: stringValue("${CLAUDE_PLUGIN_ROOT}/scripts/cleanup.sh")},
						},
					},
				},
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify hooks.json was created
	hooksPath := filepath.Join(dir, "hooks", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}

	var hooksWrapper map[string]interface{}
	if err := json.Unmarshal(data, &hooksWrapper); err != nil {
		t.Fatalf("invalid hooks JSON: %v", err)
	}

	hooks, ok := hooksWrapper["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hooks key in hooks.json")
	}

	if _, ok := hooks["PostToolUse"]; !ok {
		t.Error("expected PostToolUse in hooks")
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Error("expected Stop in hooks")
	}

	// Verify manifest references hooks
	var manifest map[string]interface{}
	json.Unmarshal([]byte(model.ManifestJSON.ValueString()), &manifest)
	if manifest["hooks"] != "./hooks/hooks.json" {
		t.Errorf("expected hooks path in manifest, got %v", manifest["hooks"])
	}
}

func TestWritePlugin_WithMcpServers(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "mcp-plugin")

	args, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"--config", "config.json"})
	envMap, _ := types.MapValueFrom(context.Background(), types.StringType, map[string]string{"DB_PATH": "/data"})

	model := &PluginResourceModel{
		Name:      stringValue("mcp-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		McpServers: []PluginMcpModel{
			{
				Name:    stringValue("plugin-db"),
				Command: stringValue("${CLAUDE_PLUGIN_ROOT}/servers/db-server"),
				Args:    args,
				Env:     envMap,
				URL:     types.StringNull(),
				Cwd:     types.StringNull(),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify .mcp.json was created
	mcpPath := filepath.Join(dir, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("failed to read .mcp.json: %v", err)
	}

	var mcpConfig map[string]interface{}
	if err := json.Unmarshal(data, &mcpConfig); err != nil {
		t.Fatalf("invalid MCP JSON: %v", err)
	}

	servers, ok := mcpConfig["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mcpServers key")
	}

	db, ok := servers["plugin-db"].(map[string]interface{})
	if !ok {
		t.Fatal("expected plugin-db server")
	}
	if db["command"] != "${CLAUDE_PLUGIN_ROOT}/servers/db-server" {
		t.Errorf("unexpected command: %v", db["command"])
	}
}

func TestWritePlugin_WithLspServers(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "lsp-plugin")

	lspArgs, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"serve"})
	extMap, _ := types.MapValueFrom(context.Background(), types.StringType, map[string]string{".go": "go"})

	model := &PluginResourceModel{
		Name:      stringValue("lsp-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		LspServers: []PluginLspModel{
			{
				Name:                  stringValue("go"),
				Command:               stringValue("gopls"),
				Args:                  lspArgs,
				Transport:             types.StringNull(),
				Env:                   types.MapNull(types.StringType),
				InitializationOptions: types.MapNull(types.StringType),
				Settings:              types.MapNull(types.StringType),
				ExtensionToLanguage:   extMap,
				RestartOnCrash:        types.BoolValue(false),
				MaxRestarts:           types.Int64Null(),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify .lsp.json was created
	lspPath := filepath.Join(dir, ".lsp.json")
	data, err := os.ReadFile(lspPath)
	if err != nil {
		t.Fatalf("failed to read .lsp.json: %v", err)
	}

	var lspConfig map[string]interface{}
	if err := json.Unmarshal(data, &lspConfig); err != nil {
		t.Fatalf("invalid LSP JSON: %v", err)
	}

	goServer, ok := lspConfig["go"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'go' key in LSP config")
	}
	if goServer["command"] != "gopls" {
		t.Errorf("unexpected command: %v", goServer["command"])
	}
	extToLang, ok := goServer["extensionToLanguage"].(map[string]interface{})
	if !ok {
		t.Fatal("expected extensionToLanguage")
	}
	if extToLang[".go"] != "go" {
		t.Errorf("unexpected extension mapping: %v", extToLang)
	}
}

func TestWritePlugin_WithExtraFiles(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "files-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("files-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Files: []PluginFileModel{
			{
				Path:       stringValue("scripts/format.sh"),
				Content:    stringValue("#!/bin/bash\necho 'formatting'"),
				SourceFile: types.StringNull(),
				Executable: types.BoolValue(true),
			},
			{
				Path:       stringValue("config/defaults.json"),
				Content:    stringValue(`{"key": "value"}`),
				SourceFile: types.StringNull(),
				Executable: types.BoolValue(false),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify script was created with executable permission
	scriptPath := filepath.Join(dir, "scripts", "format.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("failed to stat script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("expected executable permission on script")
	}
	assertFileContent(t, scriptPath, "#!/bin/bash\necho 'formatting'")

	// Verify config was created without executable permission
	configPath := filepath.Join(dir, "config", "defaults.json")
	info, err = os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config: %v", err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Error("expected non-executable permission on config")
	}
}

func TestWritePlugin_WithExtraFileFromSource(t *testing.T) {
	r := &PluginResource{}

	// Create a source file
	srcFile := filepath.Join(t.TempDir(), "source-script.sh")
	if err := os.WriteFile(srcFile, []byte("#!/bin/bash\necho 'source'"), 0o755); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "srcfile-files-plugin")
	model := &PluginResourceModel{
		Name:      stringValue("srcfile-files-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Files: []PluginFileModel{
			{
				Path:       stringValue("scripts/deploy.sh"),
				Content:    types.StringNull(),
				SourceFile: stringValue(srcFile),
				Executable: types.BoolValue(true),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	assertFileContent(t, filepath.Join(dir, "scripts", "deploy.sh"), "#!/bin/bash\necho 'source'")
}

func TestWritePlugin_InvalidFilePath(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "invalid-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("invalid-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Files: []PluginFileModel{
			{
				Path:       stringValue("../escape/bad.sh"),
				Content:    stringValue("evil"),
				SourceFile: types.StringNull(),
				Executable: types.BoolValue(false),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if !diags.HasError() {
		t.Error("expected error for path traversal")
	}
}

func TestWritePlugin_SkillMissingSourceAndContent(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "missing-plugin")

	model := &PluginResourceModel{
		Name:      stringValue("missing-plugin"),
		OutputDir: stringValue(dir),
		Version:   types.StringNull(),
		Description: types.StringNull(),
		Homepage:  types.StringNull(),
		Repository: types.StringNull(),
		License:   types.StringNull(),
		Keywords:  types.ListNull(types.StringType),
		Skills: []PluginSkillModel{
			{
				Name:      stringValue("empty-skill"),
				SourceDir: types.StringNull(),
				Content:   types.StringNull(),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if !diags.HasError() {
		t.Error("expected error when both source_dir and content are null")
	}
}

func TestWritePlugin_FullFeatured(t *testing.T) {
	r := &PluginResource{}
	dir := filepath.Join(t.TempDir(), "full-plugin")

	keywords, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"deployment", "ci-cd"})
	mcpArgs, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"--port", "8080"})
	lspArgs, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"serve"})
	extMap, _ := types.MapValueFrom(context.Background(), types.StringType, map[string]string{".go": "go"})

	model := &PluginResourceModel{
		Name:        stringValue("enterprise-tools"),
		OutputDir:   stringValue(dir),
		Version:     stringValue("2.1.0"),
		Description: stringValue("Enterprise deployment automation tools"),
		Homepage:    stringValue("https://docs.example.com"),
		Repository:  stringValue("https://github.com/example/enterprise-tools"),
		License:     stringValue("MIT"),
		Keywords:    keywords,
		Author: []AuthorModel{
			{
				Name:  stringValue("Dev Team"),
				Email: stringValue("dev@example.com"),
				URL:   types.StringNull(),
			},
		},
		Skills: []PluginSkillModel{
			{
				Name:      stringValue("code-reviewer"),
				SourceDir: types.StringNull(),
				Content:   stringValue("# Code Reviewer\n\nReview code for best practices."),
			},
		},
		Agents: []PluginAgentModel{
			{
				Name:       stringValue("security-checker"),
				SourceFile: types.StringNull(),
				Content:    stringValue("---\nname: security-checker\ndescription: Reviews code for security\n---\n\nYou are a security specialist."),
			},
		},
		Commands: []PluginCommandModel{
			{
				Name:       stringValue("status"),
				SourceFile: types.StringNull(),
				Content:    stringValue("Show deployment status."),
			},
		},
		Hooks: []PluginHooksModel{
			{
				PostToolUse: []PluginHookMatcherModel{
					{
						Matcher: stringValue("Write|Edit"),
						Hooks: []PluginHookEntryModel{
							{Type: stringValue("command"), Command: stringValue("${CLAUDE_PLUGIN_ROOT}/scripts/lint.sh")},
						},
					},
				},
			},
		},
		McpServers: []PluginMcpModel{
			{
				Name:    stringValue("deploy-server"),
				Command: stringValue("${CLAUDE_PLUGIN_ROOT}/servers/deploy"),
				Args:    mcpArgs,
				Env:     types.MapNull(types.StringType),
				URL:     types.StringNull(),
				Cwd:     types.StringNull(),
			},
		},
		LspServers: []PluginLspModel{
			{
				Name:                  stringValue("go"),
				Command:               stringValue("gopls"),
				Args:                  lspArgs,
				Transport:             types.StringNull(),
				Env:                   types.MapNull(types.StringType),
				InitializationOptions: types.MapNull(types.StringType),
				Settings:              types.MapNull(types.StringType),
				ExtensionToLanguage:   extMap,
				RestartOnCrash:        types.BoolValue(false),
				MaxRestarts:           types.Int64Null(),
			},
		},
		Files: []PluginFileModel{
			{
				Path:       stringValue("scripts/lint.sh"),
				Content:    stringValue("#!/bin/bash\necho 'linting'"),
				SourceFile: types.StringNull(),
				Executable: types.BoolValue(true),
			},
		},
	}

	diags := r.writePlugin(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	// Verify all files exist
	files := []string{
		".claude-plugin/plugin.json",
		"skills/code-reviewer/SKILL.md",
		"agents/security-checker.md",
		"commands/status.md",
		"hooks/hooks.json",
		".mcp.json",
		".lsp.json",
		"scripts/lint.sh",
	}
	for _, f := range files {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file to exist: %s", f)
		}
	}

	// Verify manifest has all component references
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(model.ManifestJSON.ValueString()), &manifest); err != nil {
		t.Fatalf("invalid manifest JSON: %v", err)
	}

	if manifest["name"] != "enterprise-tools" {
		t.Error("expected name in manifest")
	}
	if manifest["version"] != "2.1.0" {
		t.Error("expected version in manifest")
	}
	if manifest["license"] != "MIT" {
		t.Error("expected license in manifest")
	}
	if manifest["skills"] == nil {
		t.Error("expected skills in manifest")
	}
	if manifest["agents"] == nil {
		t.Error("expected agents in manifest")
	}
	if manifest["commands"] == nil {
		t.Error("expected commands in manifest")
	}
	if manifest["hooks"] == nil {
		t.Error("expected hooks reference in manifest")
	}
	if manifest["mcpServers"] == nil {
		t.Error("expected mcpServers reference in manifest")
	}
	if manifest["lspServers"] == nil {
		t.Error("expected lspServers reference in manifest")
	}
}

// --------------------------------------------------------------------------
// buildHooksJSON tests
// --------------------------------------------------------------------------

func TestBuildHooksJSON_Empty(t *testing.T) {
	r := &PluginResource{}
	hooks := PluginHooksModel{}
	result := r.buildHooksJSON(hooks)
	if len(result) != 0 {
		t.Errorf("expected empty hooks, got %d entries", len(result))
	}
}

func TestBuildHooksJSON_AllEvents(t *testing.T) {
	r := &PluginResource{}

	matcher := func() []PluginHookMatcherModel {
		return []PluginHookMatcherModel{
			{
				Matcher: stringValue(".*"),
				Hooks: []PluginHookEntryModel{
					{Type: stringValue("command"), Command: stringValue("echo test")},
				},
			},
		}
	}

	hooks := PluginHooksModel{
		PreToolUse:       matcher(),
		PostToolUse:      matcher(),
		PostToolUseFail:  matcher(),
		UserPromptSubmit: matcher(),
		Notification:     matcher(),
		Stop:             matcher(),
		SubagentStart:    matcher(),
		SubagentStop:     matcher(),
		SessionStart:     matcher(),
		SessionEnd:       matcher(),
		PreCompact:       matcher(),
	}

	result := r.buildHooksJSON(hooks)
	expectedEvents := []string{
		"PreToolUse", "PostToolUse", "PostToolUseFailure",
		"UserPromptSubmit", "Notification", "Stop",
		"SubagentStart", "SubagentStop",
		"SessionStart", "SessionEnd", "PreCompact",
	}

	for _, event := range expectedEvents {
		if _, ok := result[event]; !ok {
			t.Errorf("expected event %q in hooks output", event)
		}
	}
}

// --------------------------------------------------------------------------
// copyDirectory tests
// --------------------------------------------------------------------------

func TestCopyDirectory(t *testing.T) {
	srcDir := t.TempDir()

	// Create nested structure
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "sub", "file2.txt"), []byte("content2"), 0o644)

	dstDir := filepath.Join(t.TempDir(), "copy")
	diags := copyDirectory(srcDir, dstDir)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	assertFileContent(t, filepath.Join(dstDir, "file1.txt"), "content1")
	assertFileContent(t, filepath.Join(dstDir, "sub", "file2.txt"), "content2")
}

func TestCopyDirectory_NonExistent(t *testing.T) {
	diags := copyDirectory("/nonexistent/path", t.TempDir())
	if !diags.HasError() {
		t.Error("expected error for non-existent source")
	}
}

// --------------------------------------------------------------------------
// copyFile tests
// --------------------------------------------------------------------------

func TestCopyFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.txt")
	os.WriteFile(src, []byte("hello"), 0o644)

	dst := filepath.Join(t.TempDir(), "dst.txt")
	diags := copyFile(src, dst)
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}

	assertFileContent(t, dst, "hello")
}

func TestCopyFile_NonExistent(t *testing.T) {
	diags := copyFile("/nonexistent/file.txt", filepath.Join(t.TempDir(), "dst.txt"))
	if !diags.HasError() {
		t.Error("expected error for non-existent source")
	}
}

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

func stringValue(s string) types.String {
	return types.StringValue(s)
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %q: %v", path, err)
	}
	if string(data) != expected {
		t.Errorf("expected content %q, got %q", expected, string(data))
	}
}
