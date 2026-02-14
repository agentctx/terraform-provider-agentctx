package anthropic

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// CreateVersion uploads the bundle files from sourceDir as a multipart form
// and creates a new version for the given skill.
func (c *Client) CreateVersion(ctx context.Context, skillID string, sourceDir string) (*SkillVersion, error) {
	path := fmt.Sprintf("/v1/skills/%s/versions", skillID)

	buildBody := func() (io.Reader, string, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		// Walk the source directory and add each file as a form file part.
		absRoot, err := filepath.Abs(sourceDir)
		if err != nil {
			return nil, "", fmt.Errorf("resolve source dir: %w", err)
		}

		// The API expects files nested under a top-level directory.
		dirName := filepath.Base(absRoot)

		err = filepath.Walk(absRoot, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(absRoot, path)
			if err != nil {
				return fmt.Errorf("compute relative path: %w", err)
			}
			rel = dirName + "/" + filepath.ToSlash(rel)

			part, err := writer.CreateFormFile("files[]", rel)
			if err != nil {
				return fmt.Errorf("create form file %q: %w", rel, err)
			}

			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open file %q: %w", rel, err)
			}
			defer f.Close()

			if _, err := io.Copy(part, f); err != nil {
				return fmt.Errorf("copy file %q: %w", rel, err)
			}

			return nil
		})
		if err != nil {
			return nil, "", fmt.Errorf("walk source dir: %w", err)
		}

		if err := writer.Close(); err != nil {
			return nil, "", fmt.Errorf("close multipart writer: %w", err)
		}

		return &buf, writer.FormDataContentType(), nil
	}

	var version SkillVersion
	if err := c.doMultipart(ctx, http.MethodPost, path, buildBody, &version); err != nil {
		return nil, fmt.Errorf("create version for skill %q: %w", skillID, err)
	}
	return &version, nil
}

// GetVersion retrieves a specific version of a skill.
func (c *Client) GetVersion(ctx context.Context, skillID string, version string) (*SkillVersion, error) {
	path := fmt.Sprintf("/v1/skills/%s/versions/%s", skillID, version)
	var sv SkillVersion
	if err := c.do(ctx, http.MethodGet, path, nil, &sv); err != nil {
		return nil, fmt.Errorf("get version %q for skill %q: %w", version, skillID, err)
	}
	return &sv, nil
}

// ListVersions returns all versions for the given skill.
func (c *Client) ListVersions(ctx context.Context, skillID string) ([]SkillVersion, error) {
	path := fmt.Sprintf("/v1/skills/%s/versions", skillID)
	var resp ListVersionsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("list versions for skill %q: %w", skillID, err)
	}
	return resp.Data, nil
}

// DeleteVersion deletes a specific version of a skill.
func (c *Client) DeleteVersion(ctx context.Context, skillID string, version string) error {
	path := fmt.Sprintf("/v1/skills/%s/versions/%s", skillID, version)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete version %q for skill %q: %w", version, skillID, err)
	}
	return nil
}

// DownloadBundle downloads the bundle for a specific skill version and returns
// a map of relative file paths to their contents.
//
// The API is expected to return a tar.gz archive containing the bundle files.
func (c *Client) DownloadBundle(ctx context.Context, skillID string, version string) (map[string][]byte, error) {
	path := fmt.Sprintf("/v1/skills/%s/versions/%s/bundle", skillID, version)

	data, err := c.doRaw(ctx, http.MethodGet, path)
	if err != nil {
		return nil, fmt.Errorf("download bundle for skill %q version %q: %w", skillID, version, err)
	}

	files, err := extractTarGz(data)
	if err != nil {
		return nil, fmt.Errorf("extract bundle for skill %q version %q: %w", skillID, version, err)
	}

	return files, nil
}

// extractTarGz decompresses a gzipped tar archive and returns a map of
// file paths to their contents.
func extractTarGz(data []byte) (map[string][]byte, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	files := make(map[string][]byte)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}

		// Skip directories and non-regular files.
		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.ToSlash(header.Name)

		content, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("read tar entry %q: %w", name, err)
		}

		files[name] = content
	}

	return files, nil
}
