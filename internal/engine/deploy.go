package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
	"github.com/agentctx/terraform-provider-agentctx/internal/deployid"
	"github.com/agentctx/terraform-provider-agentctx/internal/manifest"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// Deploy executes the 6-step commit protocol to deploy a skill bundle to
// a single target per spec section 7.1.
//
// Steps:
//  1. Generate deployment_id
//  2. Clean up any previously staged deployment
//  3. Upload all bundle files in parallel
//  4. Build and upload manifest.json
//  5. Write/overwrite the ACTIVE pointer
//  6. Return DeployResult
func (e *Engine) Deploy(ctx context.Context, tgt target.Target, input DeployInput) (*DeployResult, error) {
	// Step 1: Generate deployment ID.
	depID := deployid.New()

	// Step 2: Clean up any previously staged deployment from a prior failed run.
	if input.StagedDeployID != "" {
		if err := e.CleanupStaged(ctx, tgt, input.SkillName, input.StagedDeployID); err != nil {
			// Log but do not fail — staged cleanup is best-effort.
			// The deployment can proceed even if cleanup fails.
			_ = err
		}
	}

	// Build key prefixes.
	deployPrefix := deploymentPrefix(input.SkillName, depID)

	// Step 3: Upload all files in parallel, bounded by the semaphore.
	if err := e.uploadFiles(ctx, tgt, input, deployPrefix); err != nil {
		return nil, fmt.Errorf("engine: upload files: %w", err)
	}

	// Step 4: Build and upload manifest.json.
	manifestJSON, err := e.uploadManifest(ctx, tgt, input, depID, deployPrefix)
	if err != nil {
		return nil, fmt.Errorf("engine: upload manifest: %w", err)
	}

	// Step 5: Write the ACTIVE pointer.
	if err := e.writeActivePointer(ctx, tgt, input, depID); err != nil {
		return nil, fmt.Errorf("engine: write ACTIVE: %w", err)
	}

	// Step 6: Return the result.
	return &DeployResult{
		TargetName:   tgt.Name(),
		DeploymentID: depID,
		BundleHash:   input.Bundle.BundleHash,
		ManifestJSON: manifestJSON,
	}, nil
}

// uploadFiles uploads all bundle files to the target in parallel, bounded
// by the engine's semaphore.
func (e *Engine) uploadFiles(ctx context.Context, tgt target.Target, input DeployInput, deployPrefix string) error {
	g, gctx := errgroup.WithContext(ctx)

	for _, fe := range input.Bundle.Files {
		fe := fe // capture loop variable
		g.Go(func() error {
			// Acquire semaphore slot.
			if err := e.sem.Acquire(gctx, 1); err != nil {
				return fmt.Errorf("acquire semaphore for %q: %w", fe.RelPath, err)
			}
			defer e.sem.Release(1)

			// Read file content.
			content, err := readFileContent(input, fe)
			if err != nil {
				return fmt.Errorf("read file %q: %w", fe.RelPath, err)
			}

			// Build the object key.
			key := deployPrefix + "files/" + fe.RelPath

			// Determine content type.
			ct := bundle.ContentTypeForFile(fe.RelPath)

			// Upload.
			if err := tgt.Put(gctx, key, bytes.NewReader(content), target.PutOptions{
				ContentType: ct,
			}); err != nil {
				return fmt.Errorf("put %q: %w", key, err)
			}

			return nil
		})
	}

	return g.Wait()
}

// readFileContent reads the content of a file from the bundle. If the bundle
// has an on-disk source directory (AbsPath is set), it reads from disk.
func readFileContent(input DeployInput, fe bundle.FileEntry) ([]byte, error) {
	if fe.AbsPath != "" {
		return os.ReadFile(fe.AbsPath)
	}

	// Fall back to SourceDir + RelPath for bundles without AbsPath.
	if input.SourceDir != "" {
		absPath := filepath.Join(input.SourceDir, filepath.FromSlash(fe.RelPath))
		return os.ReadFile(absPath)
	}

	return nil, fmt.Errorf("no source path available for file %q", fe.RelPath)
}

// uploadManifest builds and uploads the manifest.json for the deployment.
func (e *Engine) uploadManifest(ctx context.Context, tgt target.Target, input DeployInput, depID string, deployPrefix string) ([]byte, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Build the files map: relpath -> hash.
	files := make(map[string]string, len(input.Bundle.Files))
	for _, fe := range input.Bundle.Files {
		hash, ok := input.Bundle.FileHashes[fe.RelPath]
		if !ok {
			return nil, fmt.Errorf("missing hash for file %q", fe.RelPath)
		}
		files[fe.RelPath] = hash
	}

	m := &manifest.Manifest{
		SchemaVersion:   2,
		ProviderVersion: input.ProviderVersion,
		ResourceType:    "skill",
		ResourceName:    input.ResourceName,
		CanonicalStore:  input.CanonicalStore,
		DeploymentID:    depID,
		CreatedAt:       now,
		SourceHash:      input.Bundle.BundleHash, // source_hash = bundle_hash for source-canonical
		BundleHash:      input.Bundle.BundleHash,
		Origin: &manifest.ManifestOrigin{
			Type:      originType(input),
			SourceDir: input.SourceDir,
		},
		Registry: input.RegistryInfo,
		Files:    files,
	}

	manifestJSON, err := manifest.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	key := deployPrefix + "manifest.json"
	if err := tgt.Put(ctx, key, bytes.NewReader(manifestJSON), target.PutOptions{
		ContentType: bundle.ContentTypeManifest,
	}); err != nil {
		return nil, fmt.Errorf("put manifest: %w", err)
	}

	return manifestJSON, nil
}

// writeActivePointer writes (or conditionally overwrites) the ACTIVE pointer
// file for the skill.
func (e *Engine) writeActivePointer(ctx context.Context, tgt target.Target, input DeployInput, depID string) error {
	activeKey := activePointerKey(input.SkillName)
	body := []byte(depID)
	opts := target.PutOptions{
		ContentType: bundle.ContentTypeACTIVE,
	}

	if input.PreviousDeployID != "" {
		// Conditional write: read current ACTIVE to get ETag/Generation,
		// then use ConditionalPut to avoid clobbering concurrent updates.
		_, meta, err := tgt.Get(ctx, activeKey)
		if err != nil {
			if errors.Is(err, target.ErrNotFound) {
				// ACTIVE disappeared — fall through to unconditional put.
				return tgt.Put(ctx, activeKey, bytes.NewReader(body), opts)
			}
			return fmt.Errorf("read current ACTIVE: %w", err)
		}

		condition := target.WriteCondition{
			IfMatch:    meta.ETag,
			Generation: meta.Generation,
		}

		if err := tgt.ConditionalPut(ctx, activeKey, bytes.NewReader(body), condition, opts); err != nil {
			return fmt.Errorf("conditional put ACTIVE: %w", err)
		}
		return nil
	}

	// First deploy — unconditional write.
	return tgt.Put(ctx, activeKey, bytes.NewReader(body), opts)
}

// originType determines the origin type string for the manifest.
func originType(input DeployInput) string {
	if input.RegistryInfo != nil {
		return "registry"
	}
	return "source"
}

// activePointerKey returns the object key for the ACTIVE pointer.
func activePointerKey(skillName string) string {
	return skillName + "/.agentctx/ACTIVE"
}

// deploymentPrefix returns the object key prefix for a deployment.
func deploymentPrefix(skillName, deploymentID string) string {
	return skillName + "/.agentctx/deployments/" + deploymentID + "/"
}

// agentctxPrefix returns the prefix for all agentctx-managed objects under a skill.
func agentctxPrefix(skillName string) string {
	return skillName + "/.agentctx/"
}

// skillPrefix returns the top-level prefix for a skill (includes all content).
func skillPrefix(skillName string) string {
	return skillName + "/"
}

// readActiveDeploymentID reads the ACTIVE pointer and returns the deployment ID.
// Returns empty string and nil error if ACTIVE does not exist.
func readActiveDeploymentID(ctx context.Context, tgt target.Target, skillName string) (string, error) {
	activeKey := activePointerKey(skillName)
	rc, _, err := tgt.Get(ctx, activeKey)
	if err != nil {
		if errors.Is(err, target.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("read ACTIVE: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read ACTIVE body: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}
