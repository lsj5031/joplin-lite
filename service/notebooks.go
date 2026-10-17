package service

import "github.com/lsj5031/joplin-lite/internal/models"

// NotebookPage is a page of notebooks (Joplin's UI name for folders — the
// synced item type stays models.Folder).
type NotebookPage struct {
	Notebooks []*models.Folder `json:"notebooks"`
	HasMore   bool             `json:"has_more"`
}

// NotebookTree returns the notebook hierarchy (parents before children).
func (s *Service) NotebookTree() ([]*models.Folder, error) {
	return s.db.FolderTree()
}

// ListNotebooks lists notebooks flat, newest first.
func (s *Service) ListNotebooks(p Page) (NotebookPage, error) {
	folders, hasMore, err := s.db.ListFolders("", "", p.Limit, p.Offset)
	if err != nil {
		return NotebookPage{}, err
	}
	if folders == nil {
		folders = []*models.Folder{}
	}
	return NotebookPage{Notebooks: folders, HasMore: hasMore}, nil
}

// GetNotebook returns a notebook (nil when missing).
func (s *Service) GetNotebook(id string) (*models.Folder, error) {
	return s.db.GetFolder(id)
}

// CreateNotebook creates a notebook (timestamps/ID are filled in when empty).
func (s *Service) CreateNotebook(f *models.Folder) error {
	return s.db.CreateFolder(f)
}

// UpdateNotebook updates a notebook.
func (s *Service) UpdateNotebook(f *models.Folder) error {
	return s.db.UpdateFolder(f)
}

// DeleteNotebook deletes a notebook; deletion propagates on the next sync.
func (s *Service) DeleteNotebook(id string) error {
	return s.db.DeleteFolder(id)
}
