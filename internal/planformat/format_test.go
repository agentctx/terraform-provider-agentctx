package planformat

import (
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	p := &Plan{
		ResourceAddress: "agentctx_skill.my_skill",
		DeploymentID:    "dep_20260213T200102Z_6f2c9a1b",
		BundleHash:      "sha256:abcdef0123456789abcdef0123456789",
		SourceDir:       "/home/user/project/skills/my_skill",
		FileChanges: []FileChange{
			{
				RelPath:   "main.py",
				Action:    ActionCreate,
				NewHash:   "sha256:1111111111111111",
				SizeBytes: 1024,
			},
			{
				RelPath:   "config.yaml",
				Action:    ActionCreate,
				NewHash:   "sha256:2222222222222222",
				SizeBytes: 256,
			},
			{
				RelPath: "lib/utils.py",
				Action:  ActionUpdate,
				OldHash: "sha256:3333333333333333",
				NewHash: "sha256:4444444444444444",
			},
			{
				RelPath: "old_file.py",
				Action:  ActionDestroy,
				OldHash: "sha256:5555555555555555",
			},
		},
		Targets: []TargetAction{
			{
				TargetName:   "production",
				Action:       ActionUpdate,
				FilesAdded:   2,
				FilesChanged: 1,
				FilesDeleted: 1,
			},
		},
	}

	output := Format(p)

	// Verify header lines.
	if !strings.Contains(output, "# agentctx_skill.my_skill will be updated") {
		t.Error("missing resource address in header")
	}
	if !strings.Contains(output, "# deployment_id: dep_20260213T200102Z_6f2c9a1b") {
		t.Error("missing deployment_id in header")
	}
	if !strings.Contains(output, "# source_dir:") {
		t.Error("missing source_dir in header")
	}

	// Verify file change symbols.
	if !strings.Contains(output, "+ main.py") {
		t.Error("missing + symbol for created file main.py")
	}
	if !strings.Contains(output, "+ config.yaml") {
		t.Error("missing + symbol for created file config.yaml")
	}
	if !strings.Contains(output, "~ lib/utils.py") {
		t.Error("missing ~ symbol for updated file lib/utils.py")
	}
	if !strings.Contains(output, "- old_file.py") {
		t.Error("missing - symbol for destroyed file old_file.py")
	}

	// Verify summary line.
	if !strings.Contains(output, "2 to add, 1 to change, 1 to destroy") {
		t.Errorf("missing or incorrect summary line in output:\n%s", output)
	}

	// Verify target action.
	if !strings.Contains(output, "~ production") {
		t.Error("missing ~ symbol for target production")
	}
	if !strings.Contains(output, "+2 ~1 -1 files") {
		t.Errorf("missing target file counts in output:\n%s", output)
	}

	// Verify hash truncation appears for created/updated files (they have NewHash).
	if !strings.Contains(output, "(sha256:11111111)") {
		t.Errorf("missing truncated hash for main.py in output:\n%s", output)
	}
	if !strings.Contains(output, "(sha256:44444444)") {
		t.Errorf("missing truncated hash for lib/utils.py in output:\n%s", output)
	}

	// Destroyed file has no NewHash, so no hash should appear for old_file.py line.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "- old_file.py") && strings.Contains(line, "sha256:") {
			t.Error("destroyed file should not show a hash")
		}
	}
}

func TestFormatSummary(t *testing.T) {
	p := &Plan{
		ResourceAddress: "agentctx_skill.chatbot",
		FileChanges: []FileChange{
			{RelPath: "a.py", Action: ActionCreate},
			{RelPath: "b.py", Action: ActionCreate},
			{RelPath: "c.py", Action: ActionUpdate},
			{RelPath: "d.py", Action: ActionDestroy},
			{RelPath: "e.py", Action: ActionDestroy},
			{RelPath: "f.py", Action: ActionDestroy},
		},
		Targets: []TargetAction{
			{TargetName: "prod", Action: ActionUpdate},
			{TargetName: "staging", Action: ActionUpdate},
		},
	}

	summary := FormatSummary(p)

	want := "agentctx_skill.chatbot: 2 file(s) to add, 1 to change, 3 to destroy across 2 target(s)"
	if summary != want {
		t.Errorf("FormatSummary() =\n  %q\nwant:\n  %q", summary, want)
	}
}

func TestFormatEmpty(t *testing.T) {
	p := &Plan{
		ResourceAddress: "agentctx_skill.empty",
		DeploymentID:    "dep_20260213T200102Z_00000000",
		BundleHash:      "sha256:0000000000000000",
		FileChanges:     nil,
		Targets:         nil,
	}

	output := Format(p)

	// Should contain header.
	if !strings.Contains(output, "# agentctx_skill.empty will be updated") {
		t.Error("missing resource address in header")
	}

	// Should state no file changes.
	if !strings.Contains(output, "No file changes.") {
		t.Errorf("expected 'No file changes.' in output:\n%s", output)
	}

	// Should NOT contain file change symbols.
	if strings.Contains(output, "+ ") || strings.Contains(output, "~ ") || strings.Contains(output, "- ") {
		t.Errorf("unexpected file change symbols in empty plan output:\n%s", output)
	}

	// Should NOT contain "to add" summary since there are no file changes.
	if strings.Contains(output, "to add") {
		t.Errorf("unexpected summary line in empty plan output:\n%s", output)
	}

	// Should NOT contain target actions section.
	if strings.Contains(output, "Target actions:") {
		t.Errorf("unexpected target actions in empty plan output:\n%s", output)
	}
}

func TestFormatEmptySummary(t *testing.T) {
	p := &Plan{
		ResourceAddress: "agentctx_skill.noop",
		FileChanges:     nil,
		Targets:         nil,
	}

	summary := FormatSummary(p)
	want := "agentctx_skill.noop: 0 file(s) to add, 0 to change, 0 to destroy across 0 target(s)"
	if summary != want {
		t.Errorf("FormatSummary() =\n  %q\nwant:\n  %q", summary, want)
	}
}

func TestFormatNoSourceDir(t *testing.T) {
	p := &Plan{
		ResourceAddress: "agentctx_skill.test",
		DeploymentID:    "dep_20260213T200102Z_aabbccdd",
		BundleHash:      "sha256:abcdef01",
		SourceDir:       "",
		FileChanges:     nil,
	}

	output := Format(p)
	if strings.Contains(output, "source_dir") {
		t.Errorf("source_dir should be omitted when empty, got:\n%s", output)
	}
}
