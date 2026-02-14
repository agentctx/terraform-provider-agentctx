// Package bundle implements source-directory bundling: enumeration, exclusion,
// hashing, symlink validation, and content-type mapping.
package bundle

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// securityExcludes are hardcoded and cannot be overridden. They prevent
// secrets and credentials from being bundled.
var securityExcludes = []excludeRule{
	// Directories
	{prefix: ".git/", exact: ".git"},
	{prefix: ".aws/", exact: ".aws"},
	{prefix: ".ssh/", exact: ".ssh"},
	// Dotenv files — but NOT .env.example / .env.template
	{matchFunc: matchDotEnv},
	// Private key / certificate stores
	{glob: "**.pem"},
	{glob: "**.key"},
	{glob: "**.p12"},
	{glob: "**.pfx"},
	{glob: "**.jks"},
	// SSH keys (bare names, anywhere in tree)
	{matchFunc: matchSSHKey},
}

// convenienceExcludes are hardcoded but purely for developer convenience.
var convenienceExcludes = []excludeRule{
	{prefix: "node_modules/", exact: "node_modules"},
	{prefix: ".venv/", exact: ".venv"},
	{prefix: "__pycache__/", exact: "__pycache__"},
	{exact: ".DS_Store", matchFunc: matchBasename(".DS_Store")},
	{exact: "Thumbs.db", matchFunc: matchBasename("Thumbs.db")},
	{prefix: ".terraform/", exact: ".terraform"},
	{glob: "**.tfstate*"},
}

// excludeRule represents one exclusion condition. At most one of the fields
// is set; they are checked in order: matchFunc, prefix+exact, glob.
type excludeRule struct {
	prefix    string                   // match any path that starts with this
	exact     string                   // match the path exactly (for top-level entries)
	glob      string                   // doublestar glob pattern
	matchFunc func(relPath string) bool // custom function
}

// matchDotEnv matches .env and .env.* except .env.example and .env.template.
func matchDotEnv(relPath string) bool {
	base := filepath.Base(relPath)
	if base == ".env" {
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		suffix := base[len(".env."):]
		if suffix == "example" || suffix == "template" {
			return false
		}
		return true
	}
	return false
}

// matchSSHKey matches id_rsa or id_ed25519 anywhere in the tree.
func matchSSHKey(relPath string) bool {
	base := filepath.Base(relPath)
	return base == "id_rsa" || base == "id_ed25519"
}

// matchBasename returns a matchFunc that matches a specific basename anywhere.
func matchBasename(name string) func(string) bool {
	return func(relPath string) bool {
		return filepath.Base(relPath) == name
	}
}

// ruleMatches reports whether a single rule matches relPath.
func ruleMatches(r excludeRule, relPath string) bool {
	// Normalise to forward slashes for consistent matching.
	rel := filepath.ToSlash(relPath)

	if r.matchFunc != nil {
		return r.matchFunc(rel)
	}

	if r.prefix != "" && strings.HasPrefix(rel, r.prefix) {
		return true
	}
	if r.exact != "" && rel == r.exact {
		return true
	}

	if r.glob != "" {
		matched, _ := doublestar.Match(r.glob, rel)
		return matched
	}

	return false
}

// ShouldExclude reports whether relPath (relative to source_dir, using
// forward-slash separators) should be excluded from the bundle.
//
// It checks hardcoded security excludes, convenience excludes, and any
// user-supplied gitignore-style glob patterns (additive).
func ShouldExclude(relPath string, userExcludes []string) bool {
	rel := filepath.ToSlash(relPath)

	// Security excludes — cannot be disabled.
	for _, r := range securityExcludes {
		if ruleMatches(r, rel) {
			return true
		}
	}

	// Convenience excludes.
	for _, r := range convenienceExcludes {
		if ruleMatches(r, rel) {
			return true
		}
	}

	// User excludes — additive gitignore-style globs.
	for _, pattern := range userExcludes {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}

		p := filepath.ToSlash(pattern)

		// Directory-style patterns: "foo/" matches "foo" and "foo/**".
		if strings.HasSuffix(p, "/") {
			dir := strings.TrimSuffix(p, "/")
			if rel == dir || strings.HasPrefix(rel, dir+"/") {
				return true
			}
			continue
		}

		// Try matching as a doublestar glob.
		// If the pattern has no path separators, match against basename as well.
		if matched, _ := doublestar.Match(p, rel); matched {
			return true
		}
		if !strings.Contains(p, "/") {
			base := filepath.Base(rel)
			if matched, _ := doublestar.Match(p, base); matched {
				return true
			}
		}
	}

	return false
}

// ShouldExcludeDir is a convenience wrapper for directory-level short-circuit
// during tree walking. If the directory itself should be excluded the walker
// can skip the entire subtree.
func ShouldExcludeDir(relDir string, userExcludes []string) bool {
	rel := filepath.ToSlash(relDir)
	if rel == "." || rel == "" {
		return false
	}

	// Check if this directory path (with trailing slash) or the bare name
	// matches any rule.
	return ShouldExclude(rel, userExcludes) || ShouldExclude(rel+"/placeholder", userExcludes)
}
