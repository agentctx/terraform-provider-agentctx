// Package engine orchestrates deploying skill bundles to cloud object storage
// targets using a commit protocol. It handles deployment, refresh, pruning,
// and destruction operations.
package engine

import (
	"github.com/agentctx/terraform-provider-agentctx/internal/bundle"
	"github.com/agentctx/terraform-provider-agentctx/internal/manifest"
	"golang.org/x/sync/semaphore"
)

// Engine orchestrates deploy, refresh, prune, and destroy operations
// against cloud storage targets. It uses a weighted semaphore to bound
// concurrency across parallel file uploads.
type Engine struct {
	sem *semaphore.Weighted
}

// New creates a new Engine with the given concurrency semaphore.
func New(sem *semaphore.Weighted) *Engine {
	return &Engine{sem: sem}
}

// DeployResult holds the outcome of deploying to a single target.
type DeployResult struct {
	TargetName   string
	DeploymentID string
	BundleHash   string
	ManifestJSON []byte
}

// RefreshResult holds the state read from a target.
type RefreshResult struct {
	TargetName         string
	ActiveDeploymentID string
	Manifest           *manifest.Manifest
	Healthy            bool     // all files present
	Drifted            bool     // bundle_hash mismatch
	MissingManifest    bool
	MissingFiles       []string
}

// DeployInput holds everything needed to deploy a skill bundle to a target.
type DeployInput struct {
	SkillName       string
	Bundle          *bundle.Bundle
	CanonicalStore  string
	ProviderVersion string
	ResourceName    string
	SourceDir       string
	RegistryInfo    *manifest.ManifestRegistry // nil if no anthropic
	PreviousDeployID string                    // for conditional ACTIVE write
	StagedDeployID   string                    // from prior failed run, to clean up
}

// DestroyOptions controls how a skill is removed from a target during
// terraform destroy.
type DestroyOptions struct {
	ForceDestroy             bool
	ForceDestroySharedPrefix bool
	ManagedDeployIDs         []string // deployments created by TF
	ActiveDeployID           string   // current ACTIVE pointer value
}
