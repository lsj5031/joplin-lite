package service

import "github.com/lsj5031/joplin-lite/internal/models"

// TagPage is a page of tags.
type TagPage struct {
	Tags    []*models.Tag `json:"tags"`
	HasMore bool          `json:"has_more"`
}

// ListTags lists tags.
func (s *Service) ListTags(p Page) (TagPage, error) {
	tags, hasMore, err := s.db.ListTags("", "", p.Limit, p.Offset)
	if err != nil {
		return TagPage{}, err
	}
	if tags == nil {
		tags = []*models.Tag{}
	}
	return TagPage{Tags: tags, HasMore: hasMore}, nil
}

// GetTag returns a tag (nil when missing).
func (s *Service) GetTag(id string) (*models.Tag, error) {
	return s.db.GetTag(id)
}

// CreateTag creates a tag (timestamps/ID are filled in when empty).
func (s *Service) CreateTag(t *models.Tag) error {
	return s.db.CreateTag(t)
}

// UpdateTag updates a tag.
func (s *Service) UpdateTag(t *models.Tag) error {
	return s.db.UpdateTag(t)
}

// DeleteTag deletes a tag; deletion propagates on the next sync.
func (s *Service) DeleteTag(id string) error {
	return s.db.DeleteTag(id)
}

// NoteTags lists the tags on a note.
func (s *Service) NoteTags(noteID string) ([]*models.Tag, error) {
	tags, err := s.db.GetNoteTagsByNote(noteID)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []*models.Tag{}
	}
	return tags, nil
}

// NotesByTag lists the notes carrying a tag.
func (s *Service) NotesByTag(tagID string, p Page) (NotePage, error) {
	notes, hasMore, err := s.db.GetNotesByTag(tagID, p.Limit, p.Offset)
	if err != nil {
		return NotePage{}, err
	}
	if notes == nil {
		notes = []*models.Note{}
	}
	return NotePage{Notes: notes, HasMore: hasMore}, nil
}

// AddTag links a tag to a note.
func (s *Service) AddTag(noteID, tagID string) error {
	return s.db.AddNoteTag(noteID, tagID)
}

// RemoveTag unlinks a tag from a note.
func (s *Service) RemoveTag(noteID, tagID string) error {
	return s.db.RemoveNoteTag(noteID, tagID)
}
