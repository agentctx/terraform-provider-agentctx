package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Exclude tests
// ---------------------------------------------------------------------------

func TestShouldExclude_Security(t *testing.T) {
	securityPaths := []string{
		".git/config",
		".env",
		".env.local",
		".env.production",
		".aws/credentials",
		".ssh/id_rsa",
		"id_rsa",
		"id_ed25519",
		"server.pem",
		"tls.key",
		"keystore.p12",
		"keystore.pfx",
		"truststore.jks",
	}

	for _, p := range securityPaths {
		if !ShouldExclude(p, nil) {
			t.Errorf("expected %q to be excluded (security), but it was not", p)
		}
	}
}

func TestShouldExclude_SecurityAllowed(t *testing.T) {
	allowedPaths := []string{
		".env.example",
		".env.template",
		"subdir/.env.example",
		"subdir/.env.template",
	}

	for _, p := range allowedPaths {
		if ShouldExclude(p, nil) {
			t.Errorf("expected %q to NOT be excluded, but it was", p)
		}
	}
}

func TestShouldExclude_Convenience(t *testing.T) {
	conveniencePaths := []string{
		"node_modules/foo",
		"node_modules/bar/baz.js",
		".DS_Store",
		"subdir/.DS_Store",
		"__pycache__/bar",
		"__pycache__/module.cpython-311.pyc",
		".terraform/config",
		".terraform/plugins/linux_amd64/provider",
		"foo.tfstate",
		"foo.tfstate.backup",
		".venv/lib/python3.11/site-packages/pip",
		"Thumbs.db",
	}

	for _, p := range conveniencePaths {
		if !ShouldExclude(p, nil) {
			t.Errorf("expected %q to be excluded (convenience), but it was not", p)
		}
	}
}

func TestShouldExclude_UserGlobs(t *testing.T) {
	userExcludes := []string{"*.log", "test/", "build/**", "*.tmp"}

	cases := []struct {
		path    string
		exclude bool
	}{
		{"app.log", true},
		{"subdir/debug.log", true},
		{"test/unit.py", true},
		{"test", true},
		{"build/output.bin", true},
		{"main.py", false},
		{"README.md", false},
		{"cache.tmp", true},
	}

	for _, tc := range cases {
		got := ShouldExclude(tc.path, userExcludes)
		if got != tc.exclude {
			t.Errorf("ShouldExclude(%q, userExcludes) = %v, want %v", tc.path, got, tc.exclude)
		}
	}
}

func TestShouldExclude_Normal(t *testing.T) {
	normalPaths := []string{
		"main.py",
		"README.md",
		"src/app.go",
		"lib/utils.js",
		"config.yaml",
		"data/input.json",
		"Makefile",
		"Dockerfile",
	}

	for _, p := range normalPaths {
		if ShouldExclude(p, nil) {
			t.Errorf("expected %q to NOT be excluded, but it was", p)
		}
	}
}

func TestShouldExcludeDir(t *testing.T) {
	excludedDirs := []string{
		".git",
		"node_modules",
		".venv",
		"__pycache__",
		".terraform",
	}

	for _, d := range excludedDirs {
		if !ShouldExcludeDir(d, nil) {
			t.Errorf("expected directory %q to be excluded, but it was not", d)
		}
	}

	allowedDirs := []string{
		"src",
		"lib",
		"cmd",
		"internal",
		"pkg",
	}

	for _, d := range allowedDirs {
		if ShouldExcludeDir(d, nil) {
			t.Errorf("expected directory %q to NOT be excluded, but it was", d)
		}
	}
}

// ---------------------------------------------------------------------------
// Hash tests
// ---------------------------------------------------------------------------

func TestComputeFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	content := []byte("hello world\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := ComputeFileHash(path)
	if err != nil {
		t.Fatalf("ComputeFileHash: %v", err)
	}

	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("hash %q does not start with 'sha256:'", hash)
	}

	// Compute expected hash manually.
	h := sha256.Sum256(content)
	expected := "sha256:" + hex.EncodeToString(h[:])

	if hash != expected {
		t.Errorf("ComputeFileHash = %q, want %q", hash, expected)
	}
}

func TestComputeFileHashBytes(t *testing.T) {
	data := []byte("deterministic test input")
	hash1 := ComputeFileHashBytes(data)
	hash2 := ComputeFileHashBytes(data)

	if !strings.HasPrefix(hash1, "sha256:") {
		t.Fatalf("hash %q does not start with 'sha256:'", hash1)
	}

	if hash1 != hash2 {
		t.Errorf("ComputeFileHashBytes not deterministic: %q != %q", hash1, hash2)
	}

	// Verify against known computation.
	h := sha256.Sum256(data)
	expected := "sha256:" + hex.EncodeToString(h[:])
	if hash1 != expected {
		t.Errorf("ComputeFileHashBytes = %q, want %q", hash1, expected)
	}
}

func TestComputeBundleHash(t *testing.T) {
	files := map[string]string{
		"a.txt": "sha256:abc123",
		"b.txt": "sha256:def456",
	}

	hash := ComputeBundleHash(files)

	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("bundle hash %q does not start with 'sha256:'", hash)
	}

	// Verify deterministic: call again and check same result.
	hash2 := ComputeBundleHash(files)
	if hash != hash2 {
		t.Errorf("ComputeBundleHash not deterministic: %q != %q", hash, hash2)
	}

	// Verify against manual computation.
	// Sorted keys: "a.txt", "b.txt"
	// Entries: "a.txt\x00abc123\n" + "b.txt\x00def456\n"
	hh := sha256.New()
	hh.Write([]byte("a.txt\x00abc123\n"))
	hh.Write([]byte("b.txt\x00def456\n"))
	expected := "sha256:" + hex.EncodeToString(hh.Sum(nil))
	if hash != expected {
		t.Errorf("ComputeBundleHash = %q, want %q", hash, expected)
	}
}

func TestComputeBundleHashDeterministic(t *testing.T) {
	// Build two different maps with the same data but inserted in different
	// order. Go map iteration order is randomized, so by running this
	// enough times we exercise the sorting.
	files1 := map[string]string{
		"z/file.py":   "sha256:aaa",
		"a/file.go":   "sha256:bbb",
		"m/file.js":   "sha256:ccc",
		"b/file.yaml": "sha256:ddd",
	}

	files2 := map[string]string{
		"m/file.js":   "sha256:ccc",
		"b/file.yaml": "sha256:ddd",
		"z/file.py":   "sha256:aaa",
		"a/file.go":   "sha256:bbb",
	}

	h1 := ComputeBundleHash(files1)
	h2 := ComputeBundleHash(files2)

	if h1 != h2 {
		t.Errorf("different map insertion order produced different hashes:\n  h1=%q\n  h2=%q", h1, h2)
	}

	// Extra: run many times to exercise Go's random map iteration.
	for i := 0; i < 100; i++ {
		if ComputeBundleHash(files1) != h1 {
			t.Fatalf("iteration %d: hash changed", i)
		}
	}
}

func TestHashFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	filesOnDisk := map[string][]byte{
		"foo.txt":        []byte("foo content"),
		"subdir/bar.txt": []byte("bar content"),
	}

	for relPath, data := range filesOnDisk {
		absPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	entries := []FileEntry{
		{RelPath: "foo.txt", AbsPath: filepath.Join(dir, "foo.txt")},
		{RelPath: "subdir/bar.txt", AbsPath: filepath.Join(dir, "subdir", "bar.txt")},
	}

	fileHashes, bundleHash, err := HashFiles(dir, entries)
	if err != nil {
		t.Fatalf("HashFiles: %v", err)
	}

	// Verify per-file hashes.
	for _, entry := range entries {
		hash, ok := fileHashes[entry.RelPath]
		if !ok {
			t.Errorf("missing hash for %q", entry.RelPath)
			continue
		}
		if !strings.HasPrefix(hash, "sha256:") {
			t.Errorf("hash for %q = %q, missing sha256: prefix", entry.RelPath, hash)
		}

		// Verify against known computation.
		expected := ComputeFileHashBytes(filesOnDisk[entry.RelPath])
		if hash != expected {
			t.Errorf("hash for %q = %q, want %q", entry.RelPath, hash, expected)
		}
	}

	// Verify bundle hash.
	if !strings.HasPrefix(bundleHash, "sha256:") {
		t.Errorf("bundleHash %q missing sha256: prefix", bundleHash)
	}

	expectedBundleHash := ComputeBundleHash(fileHashes)
	if bundleHash != expectedBundleHash {
		t.Errorf("bundleHash = %q, want %q", bundleHash, expectedBundleHash)
	}
}

// ---------------------------------------------------------------------------
// Content type tests
// ---------------------------------------------------------------------------

func TestContentTypeForFile(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"README.md", "text/markdown; charset=utf-8"},
		{"config.json", "application/json"},
		{"main.py", "text/x-python"},
		{"config.yaml", "application/x-yaml"},
		{"config.yml", "application/x-yaml"},
		{"notes.txt", "text/plain; charset=utf-8"},
		{"index.html", "text/html"},
		{"data.unknown", "application/octet-stream"},
		{"binary.exe", "application/octet-stream"},
		{"noext", "application/octet-stream"},
		// Case insensitivity.
		{"README.MD", "text/markdown; charset=utf-8"},
		{"CONFIG.JSON", "application/json"},
	}

	for _, tc := range cases {
		got := ContentTypeForFile(tc.filename)
		if got != tc.want {
			t.Errorf("ContentTypeForFile(%q) = %q, want %q", tc.filename, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Enumerate tests
// ---------------------------------------------------------------------------

func TestEnumerateFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test directory structure.
	filesToCreate := []string{
		"main.py",
		"lib/utils.py",
		"lib/helpers.py",
		"config.yaml",
		"README.md",
	}

	for _, rel := range filesToCreate {
		absPath := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, []byte("content of "+rel), 0644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := EnumerateFiles(dir, nil)
	if err != nil {
		t.Fatalf("EnumerateFiles: %v", err)
	}

	// Verify all files discovered.
	if len(entries) != len(filesToCreate) {
		t.Fatalf("got %d entries, want %d", len(entries), len(filesToCreate))
	}

	// Verify sorted by RelPath.
	for i := 1; i < len(entries); i++ {
		if entries[i].RelPath < entries[i-1].RelPath {
			t.Errorf("entries not sorted: %q < %q at index %d", entries[i].RelPath, entries[i-1].RelPath, i)
		}
	}

	// Verify expected RelPaths.
	expectedSorted := make([]string, len(filesToCreate))
	copy(expectedSorted, filesToCreate)
	sort.Strings(expectedSorted)

	for i, entry := range entries {
		if entry.RelPath != expectedSorted[i] {
			t.Errorf("entries[%d].RelPath = %q, want %q", i, entry.RelPath, expectedSorted[i])
		}
		if entry.AbsPath == "" {
			t.Errorf("entries[%d].AbsPath is empty", i)
		}
	}
}

func TestEnumerateFiles_Excludes(t *testing.T) {
	dir := t.TempDir()

	// Create files that should be excluded.
	excludedDirs := []string{
		".git/config",
		".git/HEAD",
		"node_modules/express/index.js",
		"node_modules/lodash/lodash.js",
	}

	// Create files that should be included.
	includedFiles := []string{
		"main.py",
		"src/app.py",
	}

	all := append(excludedDirs, includedFiles...)
	for _, rel := range all {
		absPath := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := EnumerateFiles(dir, nil)
	if err != nil {
		t.Fatalf("EnumerateFiles: %v", err)
	}

	if len(entries) != len(includedFiles) {
		t.Fatalf("got %d entries, want %d", len(entries), len(includedFiles))
	}

	// Verify only included files are present.
	entryPaths := make(map[string]bool)
	for _, e := range entries {
		entryPaths[e.RelPath] = true
	}

	for _, rel := range includedFiles {
		if !entryPaths[rel] {
			t.Errorf("expected %q to be included, but it was not", rel)
		}
	}

	for _, rel := range excludedDirs {
		if entryPaths[rel] {
			t.Errorf("expected %q to be excluded, but it was included", rel)
		}
	}
}

func TestEnumerateFiles_Empty(t *testing.T) {
	dir := t.TempDir()

	entries, err := EnumerateFiles(dir, nil)
	if err != nil {
		t.Fatalf("EnumerateFiles: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("got %d entries for empty dir, want 0", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Symlink tests
// ---------------------------------------------------------------------------

func TestValidateSymlinks_InTree(t *testing.T) {
	dir := t.TempDir()

	// Create a real file.
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink that points within the tree.
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	entries := []FileEntry{
		{RelPath: "link.txt", AbsPath: link},
	}

	err := ValidateSymlinks(dir, entries, false)
	if err != nil {
		t.Errorf("ValidateSymlinks returned error for in-tree symlink: %v", err)
	}
}

func TestValidateSymlinks_External(t *testing.T) {
	dir := t.TempDir()
	externalDir := t.TempDir()

	// Create a file outside the source dir.
	externalFile := filepath.Join(externalDir, "external.txt")
	if err := os.WriteFile(externalFile, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink pointing outside.
	link := filepath.Join(dir, "escape.txt")
	if err := os.Symlink(externalFile, link); err != nil {
		t.Fatal(err)
	}

	entries := []FileEntry{
		{RelPath: "escape.txt", AbsPath: link},
	}

	err := ValidateSymlinks(dir, entries, false)
	if err == nil {
		t.Fatal("expected SymlinkEscapeError, got nil")
	}

	var escapeErr *SymlinkEscapeError
	if !errors.As(err, &escapeErr) {
		t.Fatalf("expected *SymlinkEscapeError, got %T: %v", err, err)
	}

	if escapeErr.Path != "escape.txt" {
		t.Errorf("SymlinkEscapeError.Path = %q, want %q", escapeErr.Path, "escape.txt")
	}

	if escapeErr.Target == "" {
		t.Error("SymlinkEscapeError.Target is empty")
	}

	// Verify the error message contains useful info.
	errMsg := escapeErr.Error()
	if !strings.Contains(errMsg, "escape.txt") {
		t.Errorf("error message %q does not mention the symlink path", errMsg)
	}
	if !strings.Contains(errMsg, "outside the source directory") {
		t.Errorf("error message %q does not mention escaping source directory", errMsg)
	}
}

func TestValidateSymlinks_AllowExternal(t *testing.T) {
	dir := t.TempDir()
	externalDir := t.TempDir()

	// Create a file outside the source dir.
	externalFile := filepath.Join(externalDir, "external.txt")
	if err := os.WriteFile(externalFile, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink pointing outside.
	link := filepath.Join(dir, "escape.txt")
	if err := os.Symlink(externalFile, link); err != nil {
		t.Fatal(err)
	}

	entries := []FileEntry{
		{RelPath: "escape.txt", AbsPath: link},
	}

	// allowExternal=true should suppress the error.
	err := ValidateSymlinks(dir, entries, true)
	if err != nil {
		t.Errorf("ValidateSymlinks with allowExternal=true returned error: %v", err)
	}
}

func TestValidateSymlinks_NoSymlinks(t *testing.T) {
	dir := t.TempDir()

	// Create regular files only.
	for _, name := range []string{"a.txt", "b.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	entries := []FileEntry{
		{RelPath: "a.txt", AbsPath: filepath.Join(dir, "a.txt")},
		{RelPath: "b.txt", AbsPath: filepath.Join(dir, "b.txt")},
	}

	err := ValidateSymlinks(dir, entries, false)
	if err != nil {
		t.Errorf("ValidateSymlinks returned error for regular files: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bundle integration tests
// ---------------------------------------------------------------------------

func TestScanBundle(t *testing.T) {
	dir := t.TempDir()

	// Create a realistic directory structure.
	files := map[string]string{
		"main.py":         "print('hello')",
		"lib/utils.py":    "def util(): pass",
		"config.yaml":     "key: value",
		"README.md":       "# My Project",
		"data/input.json": `{"key": "value"}`,
	}

	// Also create files that should be excluded.
	excluded := map[string]string{
		".git/HEAD":                    "ref: refs/heads/main",
		".git/config":                  "[core]",
		"node_modules/pkg/index.js":   "module.exports = {}",
		".env":                         "SECRET=abc",
		".terraform/terraform.tfstate": "{}",
	}

	for rel, content := range files {
		absPath := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, content := range excluded {
		absPath := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	b, err := ScanBundle(dir, nil, false)
	if err != nil {
		t.Fatalf("ScanBundle: %v", err)
	}

	// Verify SourceDir is set.
	if b.SourceDir != dir {
		t.Errorf("SourceDir = %q, want %q", b.SourceDir, dir)
	}

	// Verify correct number of files (only non-excluded ones).
	if len(b.Files) != len(files) {
		t.Errorf("got %d files, want %d", len(b.Files), len(files))
		for _, f := range b.Files {
			t.Logf("  file: %s", f.RelPath)
		}
	}

	// Verify Files are sorted.
	for i := 1; i < len(b.Files); i++ {
		if b.Files[i].RelPath < b.Files[i-1].RelPath {
			t.Errorf("Files not sorted: %q < %q", b.Files[i].RelPath, b.Files[i-1].RelPath)
		}
	}

	// Verify FileHashes populated for each file.
	for _, f := range b.Files {
		hash, ok := b.FileHashes[f.RelPath]
		if !ok {
			t.Errorf("missing hash for %q", f.RelPath)
			continue
		}
		if !strings.HasPrefix(hash, "sha256:") {
			t.Errorf("hash for %q = %q, missing sha256: prefix", f.RelPath, hash)
		}
		// Hex part should be 64 characters (256 bits).
		hexPart := strings.TrimPrefix(hash, "sha256:")
		if len(hexPart) != 64 {
			t.Errorf("hash hex for %q has length %d, want 64", f.RelPath, len(hexPart))
		}
	}

	// Verify BundleHash is present and correctly formatted.
	if !strings.HasPrefix(b.BundleHash, "sha256:") {
		t.Errorf("BundleHash %q missing sha256: prefix", b.BundleHash)
	}

	hexPart := strings.TrimPrefix(b.BundleHash, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("BundleHash hex has length %d, want 64", len(hexPart))
	}

	// Verify BundleHash matches recomputation.
	expectedBundleHash := ComputeBundleHash(b.FileHashes)
	if b.BundleHash != expectedBundleHash {
		t.Errorf("BundleHash = %q, want %q", b.BundleHash, expectedBundleHash)
	}

	// Verify excluded files are not present.
	for rel := range excluded {
		if _, ok := b.FileHashes[rel]; ok {
			t.Errorf("excluded file %q found in FileHashes", rel)
		}
	}
}

func TestBundleFromFiles(t *testing.T) {
	files := map[string][]byte{
		"main.py":      []byte("print('hello')"),
		"config.yaml":  []byte("key: value"),
		"lib/utils.py": []byte("def util(): pass"),
	}

	b := BundleFromFiles(files)

	// Verify SourceDir is empty (no on-disk source).
	if b.SourceDir != "" {
		t.Errorf("SourceDir = %q, want empty", b.SourceDir)
	}

	// Verify correct number of files.
	if len(b.Files) != len(files) {
		t.Errorf("got %d files, want %d", len(b.Files), len(files))
	}

	// Verify files are sorted.
	for i := 1; i < len(b.Files); i++ {
		if b.Files[i].RelPath < b.Files[i-1].RelPath {
			t.Errorf("Files not sorted: %q < %q", b.Files[i].RelPath, b.Files[i-1].RelPath)
		}
	}

	// Verify AbsPath is empty for all entries.
	for _, f := range b.Files {
		if f.AbsPath != "" {
			t.Errorf("AbsPath for %q = %q, want empty", f.RelPath, f.AbsPath)
		}
	}

	// Verify FileHashes computed correctly.
	for rel, data := range files {
		hash, ok := b.FileHashes[rel]
		if !ok {
			t.Errorf("missing hash for %q", rel)
			continue
		}
		expected := ComputeFileHashBytes(data)
		if hash != expected {
			t.Errorf("hash for %q = %q, want %q", rel, hash, expected)
		}
	}

	// Verify BundleHash is present.
	if !strings.HasPrefix(b.BundleHash, "sha256:") {
		t.Errorf("BundleHash %q missing sha256: prefix", b.BundleHash)
	}

	// Verify BundleHash matches recomputation.
	expectedBundleHash := ComputeBundleHash(b.FileHashes)
	if b.BundleHash != expectedBundleHash {
		t.Errorf("BundleHash = %q, want %q", b.BundleHash, expectedBundleHash)
	}

	// Verify deterministic: calling again with same input produces same hash.
	b2 := BundleFromFiles(files)
	if b.BundleHash != b2.BundleHash {
		t.Errorf("BundleFromFiles not deterministic: %q != %q", b.BundleHash, b2.BundleHash)
	}
}
