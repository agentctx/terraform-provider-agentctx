// Package planformat produces human-readable plan output per spec §13.
// It mirrors the style of "terraform plan" output, showing what files
// will be added, changed, or deleted and what deployment actions will
// be taken.
package planformat

import (
	"fmt"
	"sort"
	"strings"
)

// Action describes the type of change for a resource or file.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDestroy Action = "destroy"
	ActionNoop    Action = "no-op"
)

// FileChange describes a single file-level diff within a deployment.
type FileChange struct {
	RelPath   string
	Action    Action
	OldHash   string // empty on create
	NewHash   string // empty on destroy
	SizeBytes int64  // new size; 0 on destroy
}

// TargetAction describes what will happen on a single deployment target.
type TargetAction struct {
	TargetName string
	Action     Action
	FilesAdded int
	FilesChanged int
	FilesDeleted int
}

// Plan is the top-level container for all plan information.
type Plan struct {
	ResourceAddress string // e.g. agentctx_skill.my_skill
	DeploymentID    string
	BundleHash      string
	SourceDir       string
	FileChanges     []FileChange
	Targets         []TargetAction
}

// Format renders a Plan as a human-readable string suitable for display
// in a terminal or Terraform plan output.
func Format(p *Plan) string {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("  # %s will be updated\n", p.ResourceAddress))
	b.WriteString(fmt.Sprintf("  # deployment_id: %s\n", p.DeploymentID))
	b.WriteString(fmt.Sprintf("  # bundle_hash:   %s\n", p.BundleHash))
	if p.SourceDir != "" {
		b.WriteString(fmt.Sprintf("  # source_dir:    %s\n", p.SourceDir))
	}
	b.WriteString("\n")

	// File changes
	adds, changes, deletes := classifyFiles(p.FileChanges)
	if len(p.FileChanges) > 0 {
		b.WriteString("  File changes:\n")

		writeFileSection(&b, "+", adds)
		writeFileSection(&b, "~", changes)
		writeFileSection(&b, "-", deletes)

		b.WriteString(fmt.Sprintf("\n  %d to add, %d to change, %d to destroy.\n",
			len(adds), len(changes), len(deletes)))
		b.WriteString("\n")
	} else {
		b.WriteString("  No file changes.\n\n")
	}

	// Target actions
	if len(p.Targets) > 0 {
		b.WriteString("  Target actions:\n")
		for _, t := range p.Targets {
			symbol := actionSymbol(t.Action)
			b.WriteString(fmt.Sprintf("    %s %s", symbol, t.TargetName))
			if t.Action != ActionDestroy {
				b.WriteString(fmt.Sprintf(" (+%d ~%d -%d files)",
					t.FilesAdded, t.FilesChanged, t.FilesDeleted))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// FormatSummary returns a single-line summary of the plan.
func FormatSummary(p *Plan) string {
	adds, changes, deletes := classifyFiles(p.FileChanges)
	return fmt.Sprintf("%s: %d file(s) to add, %d to change, %d to destroy across %d target(s)",
		p.ResourceAddress, len(adds), len(changes), len(deletes), len(p.Targets))
}

func classifyFiles(files []FileChange) (adds, changes, deletes []FileChange) {
	for _, f := range files {
		switch f.Action {
		case ActionCreate:
			adds = append(adds, f)
		case ActionUpdate:
			changes = append(changes, f)
		case ActionDestroy:
			deletes = append(deletes, f)
		}
	}
	// Sort each group by RelPath for determinism.
	sortFiles := func(s []FileChange) {
		sort.Slice(s, func(i, j int) bool { return s[i].RelPath < s[j].RelPath })
	}
	sortFiles(adds)
	sortFiles(changes)
	sortFiles(deletes)
	return
}

func writeFileSection(b *strings.Builder, symbol string, files []FileChange) {
	for _, f := range files {
		b.WriteString(fmt.Sprintf("    %s %s", symbol, f.RelPath))
		if f.NewHash != "" {
			b.WriteString(fmt.Sprintf("  (%s)", truncateHash(f.NewHash)))
		}
		b.WriteString("\n")
	}
}

func actionSymbol(a Action) string {
	switch a {
	case ActionCreate:
		return "+"
	case ActionUpdate:
		return "~"
	case ActionDestroy:
		return "-"
	case ActionNoop:
		return " "
	default:
		return "?"
	}
}

// truncateHash shortens a "sha256:abcdef..." hash to "sha256:abcdef01" for
// display purposes (prefix + first 8 hex chars).
func truncateHash(h string) string {
	const prefix = "sha256:"
	if strings.HasPrefix(h, prefix) {
		hex := h[len(prefix):]
		if len(hex) > 8 {
			hex = hex[:8]
		}
		return prefix + hex
	}
	if len(h) > 15 {
		return h[:15]
	}
	return h
}
