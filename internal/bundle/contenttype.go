package bundle

import (
	"path/filepath"
	"strings"
)

const (
	// ContentTypeACTIVE is the content type for the ACTIVE marker file.
	ContentTypeACTIVE = "text/plain; charset=utf-8"

	// ContentTypeManifest is the content type for the manifest JSON file.
	ContentTypeManifest = "application/json"
)

// extensionMap maps lowercase file extensions to their MIME content types
// per spec §4.4.
var extensionMap = map[string]string{
	".md":   "text/markdown; charset=utf-8",
	".json": "application/json",
	".py":   "text/x-python",
	".yaml": "application/x-yaml",
	".yml":  "application/x-yaml",
	".txt":  "text/plain; charset=utf-8",
	".html": "text/html",
}

// ContentTypeForFile returns the content type for a file based on its
// extension. If the extension is not recognized, it returns
// "application/octet-stream".
func ContentTypeForFile(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ct, ok := extensionMap[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}
