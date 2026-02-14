package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// CreateSkill creates a new skill by uploading source files.
// The sourceDir is walked and each file is uploaded as a files[] multipart field.
// An optional displayTitle can be provided as a form field.
func (c *Client) CreateSkill(ctx context.Context, sourceDir string, displayTitle string) (*Skill, error) {
	buildBody := func() (io.Reader, string, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		if displayTitle != "" {
			if err := writer.WriteField("display_title", displayTitle); err != nil {
				return nil, "", fmt.Errorf("write display_title field: %w", err)
			}
		}

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

	var skill Skill
	if err := c.doMultipart(ctx, http.MethodPost, "/v1/skills", buildBody, &skill); err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}
	return &skill, nil
}

// GetSkill retrieves a skill by its ID.
func (c *Client) GetSkill(ctx context.Context, skillID string) (*Skill, error) {
	var skill Skill
	if err := c.do(ctx, http.MethodGet, "/v1/skills/"+skillID, nil, &skill); err != nil {
		return nil, fmt.Errorf("get skill %q: %w", skillID, err)
	}
	return &skill, nil
}

// UpdateSkill updates an existing skill's metadata.
func (c *Client) UpdateSkill(ctx context.Context, skillID string, req UpdateSkillRequest) (*Skill, error) {
	var skill Skill
	if err := c.do(ctx, http.MethodPut, "/v1/skills/"+skillID, req, &skill); err != nil {
		return nil, fmt.Errorf("update skill %q: %w", skillID, err)
	}
	return &skill, nil
}

// DeleteSkill deletes a skill by its ID.
func (c *Client) DeleteSkill(ctx context.Context, skillID string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/skills/"+skillID, nil, nil); err != nil {
		return fmt.Errorf("delete skill %q: %w", skillID, err)
	}
	return nil
}
