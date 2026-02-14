package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// Destroy handles terraform destroy for a skill on a target per spec
// section 11.1.
//
// Behavior depends on the ForceDestroy and ForceDestroySharedPrefix flags:
//
//   - !ForceDestroy: delete only managed deployments and delete the ACTIVE
//     pointer only if it currently points to a managed deployment.
//
//   - ForceDestroy && !ForceDestroySharedPrefix: delete all objects under
//     <skill>/.agentctx/ (scoped to our layout).
//
//   - ForceDestroy && ForceDestroySharedPrefix: delete ALL objects under
//     <skill>/ (the entire skill prefix including any non-managed content).
func (e *Engine) Destroy(ctx context.Context, tgt target.Target, skillName string, opts DestroyOptions) error {
	if opts.ForceDestroy {
		return e.forceDestroy(ctx, tgt, skillName, opts.ForceDestroySharedPrefix)
	}
	return e.gracefulDestroy(ctx, tgt, skillName, opts)
}

// forceDestroy deletes all objects under the appropriate prefix.
func (e *Engine) forceDestroy(ctx context.Context, tgt target.Target, skillName string, includeSharedPrefix bool) error {
	var prefix string
	if includeSharedPrefix {
		prefix = skillPrefix(skillName)
	} else {
		prefix = agentctxPrefix(skillName)
	}

	objects, err := tgt.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("destroy: list %q: %w", prefix, err)
	}

	return e.deleteObjects(ctx, tgt, objects)
}

// gracefulDestroy deletes only managed deployments and conditionally
// removes the ACTIVE pointer.
func (e *Engine) gracefulDestroy(ctx context.Context, tgt target.Target, skillName string, opts DestroyOptions) error {
	// Build a set of managed deployment IDs for lookup.
	managedSet := make(map[string]struct{}, len(opts.ManagedDeployIDs))
	for _, id := range opts.ManagedDeployIDs {
		managedSet[id] = struct{}{}
	}

	// Delete each managed deployment.
	for _, depID := range opts.ManagedDeployIDs {
		if err := e.deleteDeployment(ctx, tgt, skillName, depID); err != nil {
			return fmt.Errorf("destroy: delete deployment %q: %w", depID, err)
		}
	}

	// Delete ACTIVE only if it points to a managed deployment.
	activeKey := activePointerKey(skillName)
	currentActiveID, err := readCurrentActive(ctx, tgt, activeKey)
	if err != nil {
		if errors.Is(err, target.ErrNotFound) {
			// ACTIVE doesn't exist — nothing to do.
			return nil
		}
		return fmt.Errorf("destroy: read ACTIVE: %w", err)
	}

	if _, isManaged := managedSet[currentActiveID]; isManaged {
		if err := tgt.Delete(ctx, activeKey); err != nil {
			return fmt.Errorf("destroy: delete ACTIVE: %w", err)
		}
	}

	return nil
}

// deleteObjects deletes a list of objects from a target in parallel,
// bounded by the engine's semaphore.
func (e *Engine) deleteObjects(ctx context.Context, tgt target.Target, objects []target.ObjectInfo) error {
	g, gctx := errgroup.WithContext(ctx)

	for _, obj := range objects {
		obj := obj
		g.Go(func() error {
			if err := e.sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer e.sem.Release(1)

			if err := tgt.Delete(gctx, obj.Key); err != nil {
				return fmt.Errorf("delete %q: %w", obj.Key, err)
			}
			return nil
		})
	}

	return g.Wait()
}

// readCurrentActive reads the ACTIVE pointer and returns the raw deployment
// ID string. Returns target.ErrNotFound if the ACTIVE key does not exist.
func readCurrentActive(ctx context.Context, tgt target.Target, activeKey string) (string, error) {
	rc, _, err := tgt.Get(ctx, activeKey)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read ACTIVE body: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}
