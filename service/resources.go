package service

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// AttachFile stores a local file as a Joplin resource in the vault and returns
// the created resource along with its Markdown reference (![title](:/id) for
// images or [title](:/id) for other files).
func (s *Service) AttachFile(path string) (*models.Resource, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return s.AttachBytes(filepath.Base(path), "", data)
}

// AttachBytes stores raw bytes as a Joplin resource in the vault and returns
// the created resource along with its Markdown reference.
func (s *Service) AttachBytes(filename, mimeType string, data []byte) (*models.Resource, string, error) {
	filename = strings.TrimSpace(filepath.Base(filename))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "attachment.bin"
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if mimeType == "" && ext != "" {
		mimeType = mime.TypeByExtension("." + ext)
		if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}
	}
	if mimeType == "" {
		if len(data) > 0 {
			mimeType = http.DetectContentType(data)
			if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
				mimeType = strings.TrimSpace(mimeType[:idx])
			}
		} else {
			mimeType = "application/octet-stream"
		}
	}
	if ext == "" && mimeType == "image/png" {
		ext = "png"
		if !strings.Contains(filename, ".") {
			filename += ".png"
		}
	}

	res := &models.Resource{
		Title:         filename,
		Filename:      filename,
		Mime:          mimeType,
		FileExtension: ext,
		Size:          int64(len(data)),
	}
	if err := s.db.CreateResource(res); err != nil {
		return nil, "", err
	}
	if err := s.db.SaveResourceFile(res.ID, data); err != nil {
		_ = s.db.DeleteResource(res.ID)
		return nil, "", err
	}

	ref := fmt.Sprintf("[%s](:/%s)", res.Title, res.ID)
	if strings.HasPrefix(res.Mime, "image/") {
		ref = "!" + ref
	}
	return res, ref, nil
}
