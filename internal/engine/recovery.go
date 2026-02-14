package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
	"github.com/agentctx/terraform-provider-agentctx/internal/manifest"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// CleanupStaged deletes all objects under a staged deployment prefix.
// This is used to clean up partial uploads from a prior failed run.
func (e *Engine) CleanupStaged(ctx context.Context, tgt target.Target, skillName string, stagedDeployID string) error {
	prefix := deploymentPrefix(skillName, stagedDeployID)

	objects, err := tgt.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list staged deployment %q: %w", stagedDeployID, err)
	}

	g, gctx := errgroup.WithContext(ctx)

	for _, obj := range objects {
		obj := obj
		g.Go(func() error {
			if err := e.sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer e.sem.Release(1)

			if err := tgt.Delete(gctx, obj.Key); err != nil {
				return fmt.Errorf("delete staged object %q: %w", obj.Key, err)
			}
			return nil
		})
	}

	return g.Wait()
}

// Refresh reads the current state of a skill from a target and returns
// a RefreshResult describing its health and drift status.
//
// If deepCheck is true, a HEAD request is issued for every file in the
// manifest to verify that all objects are present.
func (e *Engine) Refresh(ctx context.Context, tgt target.Target, skillName string, expectedBundleHash string, deepCheck bool) (*RefreshResult, error) {
	result := &RefreshResult{
		TargetName: tgt.Name(),
	}

	// Step 1: Read ACTIVE to get the deployment ID.
	activeDepID, err := readActiveDeploymentID(ctx, tgt, skillName)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}

	// Step 2: If ACTIVE doesn't exist, return empty result (no deployment).
	if activeDepID == "" {
		return result, nil
	}

	result.ActiveDeploymentID = activeDepID

	// Step 3: Read the manifest at the expected path.
	manifestKey := deploymentPrefix(skillName, activeDepID) + "manifest.json"
	rc, _, err := tgt.Get(ctx, manifestKey)
	if err != nil {
		if errors.Is(err, target.ErrNotFound) {
			// Step 4: Manifest missing.
			result.MissingManifest = true
			result.Healthy = false
			return result, nil
		}
		return nil, fmt.Errorf("refresh: read manifest: %w", err)
	}

	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("refresh: read manifest body: %w", err)
	}

	m, err := manifest.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("refresh: unmarshal manifest: %w", err)
	}

	result.Manifest = m

	// Step 5: Compare manifest.BundleHash vs expectedBundleHash.
	if expectedBundleHash != "" && m.BundleHash != expectedBundleHash {
		result.Drifted = true
	}

	// Step 6: If deepCheck, HEAD each file in manifest.Files.
	if deepCheck {
		missingFiles, err := e.checkFiles(ctx, tgt, skillName, activeDepID, m)
		if err != nil {
			return nil, fmt.Errorf("refresh: deep check: %w", err)
		}
		result.MissingFiles = missingFiles
	}

	// Step 7: Determine health.
	result.Healthy = !result.MissingManifest && len(result.MissingFiles) == 0

	return result, nil
}

// checkFiles issues HEAD requests for every file listed in the manifest
// and returns the relative paths of any missing files.
func (e *Engine) checkFiles(ctx context.Context, tgt target.Target, skillName string, deploymentID string, m *manifest.Manifest) ([]string, error) {
	type fileCheck struct {
		relPath string
		missing bool
	}

	checks := make([]fileCheck, 0, len(m.Files))
	for relPath := range m.Files {
		checks = append(checks, fileCheck{relPath: relPath})
	}

	g, gctx := errgroup.WithContext(ctx)

	results := make([]fileCheck, len(checks))
	copy(results, checks)

	for i := range results {
		i := i
		g.Go(func() error {
			if err := e.sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer e.sem.Release(1)

			key := deploymentPrefix(skillName, deploymentID) + "files/" + results[i].relPath
			_, err := tgt.Head(gctx, key)
			if err != nil {
				if errors.Is(err, target.ErrNotFound) {
					results[i].missing = true
					return nil
				}
				return fmt.Errorf("head %q: %w", key, err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var missing []string
	for _, fc := range results {
		if fc.missing {
			missing = append(missing, fc.relPath)
		}
	}

	return missing, nil
}

// Repair attempts to fix a broken deployment by re-uploading missing files
// and the manifest. It does not change the ACTIVE pointer.
func (e *Engine) Repair(ctx context.Context, tgt target.Target, skillName string, deploymentID string, b *bundle.Bundle, m *manifest.Manifest) error {
	deployPrefix := deploymentPrefix(skillName, deploymentID)

	// Determine which files are missing.
	missingFiles, err := e.checkFiles(ctx, tgt, skillName, deploymentID, m)
	if err != nil {
		return fmt.Errorf("repair: check files: %w", err)
	}

	// Build a set of missing files for quick lookup.
	missingSet := make(map[string]struct{}, len(missingFiles))
	for _, mf := range missingFiles {
		missingSet[mf] = struct{}{}
	}

	// Re-upload missing files.
	g, gctx := errgroup.WithContext(ctx)

	for _, fe := range b.Files {
		fe := fe
		if _, isMissing := missingSet[fe.RelPath]; !isMissing {
			continue
		}

		g.Go(func() error {
			if err := e.sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer e.sem.Release(1)

			content, err := readRepairFileContent(b, fe)
			if err != nil {
				return fmt.Errorf("repair: read file %q: %w", fe.RelPath, err)
			}

			key := deployPrefix + "files/" + fe.RelPath
			ct := bundle.ContentTypeForFile(fe.RelPath)

			if err := tgt.Put(gctx, key, bytes.NewReader(content), target.PutOptions{
				ContentType: ct,
			}); err != nil {
				return fmt.Errorf("repair: put %q: %w", key, err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Re-upload manifest if it was missing.
	manifestKey := deployPrefix + "manifest.json"
	_, headErr := tgt.Head(ctx, manifestKey)
	if headErr != nil && errors.Is(headErr, target.ErrNotFound) {
		manifestJSON, err := manifest.Marshal(m)
		if err != nil {
			return fmt.Errorf("repair: marshal manifest: %w", err)
		}
		if err := tgt.Put(ctx, manifestKey, bytes.NewReader(manifestJSON), target.PutOptions{
			ContentType: bundle.ContentTypeManifest,
		}); err != nil {
			return fmt.Errorf("repair: put manifest: %w", err)
		}
	}

	return nil
}

// readRepairFileContent reads a file for repair. It tries AbsPath first,
// then falls back to SourceDir + RelPath.
func readRepairFileContent(b *bundle.Bundle, fe bundle.FileEntry) ([]byte, error) {
	if fe.AbsPath != "" {
		return os.ReadFile(fe.AbsPath)
	}
	if b.SourceDir != "" {
		absPath := filepath.Join(b.SourceDir, filepath.FromSlash(fe.RelPath))
		return os.ReadFile(absPath)
	}
	return nil, fmt.Errorf("no source path available for file %q", fe.RelPath)
}
