package engine

import (
	"context"
	"fmt"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/agentctx/terraform-provider-agentctx/internal/deployid"
	"github.com/agentctx/terraform-provider-agentctx/internal/target"
)

// Prune removes old deployments beyond the retention limit per spec section 11.2.
//
// It filters managedDeployIDs to exclude activeDeployID, sorts the remainder
// by timestamp (oldest first), and deletes those beyond the retain count.
// Returns the list of deployment IDs that were pruned.
func (e *Engine) Prune(ctx context.Context, tgt target.Target, skillName string, activeDeployID string, managedDeployIDs []string, retain int) (pruned []string, err error) {
	// Step 1: Filter managedDeployIDs to exclude activeDeployID.
	candidates := make([]string, 0, len(managedDeployIDs))
	for _, id := range managedDeployIDs {
		if id != activeDeployID {
			candidates = append(candidates, id)
		}
	}

	// Nothing to prune if within retention limit.
	if len(candidates) <= retain {
		return nil, nil
	}

	// Step 2: Parse timestamps and sort by time (oldest first).
	type deployWithTime struct {
		id string
		ts int64 // unix timestamp for sorting
	}

	deploys := make([]deployWithTime, 0, len(candidates))
	for _, id := range candidates {
		t, err := deployid.Parse(id)
		if err != nil {
			// Skip deployment IDs we cannot parse — they may have been
			// created by a different system or are malformed.
			continue
		}
		deploys = append(deploys, deployWithTime{id: id, ts: t.Unix()})
	}

	sort.Slice(deploys, func(i, j int) bool {
		return deploys[i].ts < deploys[j].ts
	})

	// Step 3: Determine which deployments to prune.
	// We keep the newest `retain` deployments and prune the rest.
	pruneCount := len(deploys) - retain
	if pruneCount <= 0 {
		return nil, nil
	}

	toPrune := deploys[:pruneCount]

	// Step 4: Delete each deployment to prune.
	for _, dp := range toPrune {
		if err := e.deleteDeployment(ctx, tgt, skillName, dp.id); err != nil {
			return pruned, fmt.Errorf("prune deployment %q: %w", dp.id, err)
		}
		pruned = append(pruned, dp.id)
	}

	return pruned, nil
}

// deleteDeployment lists and deletes all objects under a deployment prefix.
func (e *Engine) deleteDeployment(ctx context.Context, tgt target.Target, skillName string, deploymentID string) error {
	prefix := deploymentPrefix(skillName, deploymentID)

	objects, err := tgt.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list deployment %q: %w", deploymentID, err)
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
				return fmt.Errorf("delete %q: %w", obj.Key, err)
			}
			return nil
		})
	}

	return g.Wait()
}
