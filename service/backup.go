package service

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/jex"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// ExportJEX writes the whole library to a JEX archive. The archive is built
// beside the target and renamed on success, so an interrupted export never
// leaves a truncated file that looks like a backup.
func (s *Service) ExportJEX(path string, progress func(done, total int, name string)) (*jex.ExportStats, error) {
	data := jex.ExportData{ResourceDir: s.db.ResourceDir(), Progress: progress}
	var err error
	if data.Notes, err = s.db.AllNotes(); err != nil {
		return nil, fmt.Errorf("read notes: %w", err)
	}
	if data.Folders, err = s.db.AllFolders(); err != nil {
		return nil, fmt.Errorf("read notebooks: %w", err)
	}
	if data.Resources, err = s.db.AllResources(); err != nil {
		return nil, fmt.Errorf("read attachments: %w", err)
	}
	if data.Tags, err = s.db.AllTags(); err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	if data.NoteTags, err = s.db.AllNoteTags(); err != nil {
		return nil, fmt.Errorf("read tag links: %w", err)
	}

	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("create archive: %w", err)
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	stats, err := jex.Export(bw, data)
	if err == nil {
		err = bw.Flush()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("finish archive: %w", err)
	}
	return &stats, nil
}

// ImportOptions controls ImportJEX.
type ImportOptions struct {
	// DryRun parses and resolves the archive without writing anything.
	DryRun bool
	// NotebookID, when set, receives every imported note instead of rebuilding
	// the archive's notebook tree (the same behaviour as Joplin's importer).
	NotebookID string
	// NotebookTitle names the fallback notebook used when a note's notebook is
	// missing from the archive. Defaults to the archive's file name.
	NotebookTitle string
}

// ImportResult reports what an import did (or, for a dry run, would do).
type ImportResult struct {
	Notes     int
	Folders   int
	Resources int
	Tags      int
	NoteTags  int
	Notebook  string
	Warnings  []string
}

var idPattern = regexp.MustCompile(`\b[0-9a-f]{32}\b`)

// ImportJEX reads a JEX archive into the library. Every item gets a new id;
// notebooks, attachments, tags and note bodies are re-linked to those ids.
func (s *Service) ImportJEX(path string, opts ImportOptions) (*ImportResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	archive, err := jex.ReadArchive(f)
	f.Close()
	if err != nil {
		return nil, err
	}

	res := &ImportResult{Warnings: archive.Warnings}

	// Resolve the notebook every unattached note goes to.
	destID := opts.NotebookID
	if destID != "" {
		folder, err := s.db.GetFolder(destID)
		if err != nil {
			return nil, fmt.Errorf("destination notebook: %w", err)
		}
		if folder == nil {
			return nil, fmt.Errorf("destination notebook %s does not exist", destID)
		}
	}

	// New ids for everything. Folder ids are resolved as the tree is built.
	folderIDs := map[string]string{}
	noteIDs := map[string]string{}
	resourceIDs := map[string]string{}
	tagIDs := map[string]string{}

	for _, n := range archive.Notes {
		noteIDs[n.ID] = models.NewID()
	}
	for _, r := range archive.Resources {
		resourceIDs[r.ID] = models.NewID()
	}
	for _, t := range archive.Tags {
		tagIDs[t.ID] = models.NewID()
	}

	now := time.Now().UnixMilli()
	batch := store.ImportBatch{}

	// Notebooks first: parents before children, unique titles.
	if destID == "" {
		pending := append([]*models.Folder(nil), archive.Folders...)
		for len(pending) > 0 {
			var next []*models.Folder
			progressed := false
			for _, f := range pending {
				newID := models.NewID()
				parent := f.ParentID
				if parent != "" {
					mapped, ok := folderIDs[parent]
					if !ok {
						next = append(next, f)
						continue
					}
					parent = mapped
				}
				folderIDs[f.ID] = newID
				title, err := s.uniqueFolderTitle(f.Title, parent)
				if err != nil {
					return nil, err
				}
				batch.Folders = append(batch.Folders, &models.Folder{
					ID:                   newID,
					Title:                title,
					CreatedTime:          orNow(f.CreatedTime, now),
					UpdatedTime:          orNow(f.UpdatedTime, now),
					UserCreatedTime:      orNow(f.UserCreatedTime, now),
					UserUpdatedTime:      orNow(f.UserUpdatedTime, now),
					EncryptionCipherText: f.EncryptionCipherText,
					EncryptionApplied:    f.EncryptionApplied,
					ParentID:             parent,
					IsShared:             f.IsShared,
					ShareID:              f.ShareID,
					MasterKeyID:          f.MasterKeyID,
					Icon:                 f.Icon,
					UserData:             f.UserData,
				})
				progressed = true
			}
			if !progressed {
				// Cycles or parents missing from the archive: attach the rest
				// to the root so nothing is silently dropped.
				for _, f := range next {
					res.Warnings = append(res.Warnings,
						fmt.Sprintf("notebook %q has a parent outside the archive; imported at the root", f.Title))
					f.ParentID = ""
				}
				pending = next
				continue
			}
			pending = next
		}
	}

	// Fallback notebook for notes whose notebook is absent.
	fallbackID := destID
	if fallbackID == "" {
		needed := false
		for _, n := range archive.Notes {
			if _, ok := folderIDs[n.ParentID]; !ok {
				needed = true
				break
			}
		}
		if needed {
			title := opts.NotebookTitle
			if title == "" {
				title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			}
			title, err = s.uniqueFolderTitle(title, "")
			if err != nil {
				return nil, err
			}
			fallbackID = models.NewID()
			batch.Folders = append(batch.Folders, &models.Folder{
				ID:          fallbackID,
				Title:       title,
				ParentID:    "",
				UpdatedTime: now,
			})
		}
	}

	// Notes, with notebook and body ids remapped.
	for _, n := range archive.Notes {
		parent := fallbackID
		if mapped, ok := folderIDs[n.ParentID]; ok {
			parent = mapped
		} else if destID != "" {
			parent = destID
		}
		batch.Notes = append(batch.Notes, &models.Note{
			ID:                   noteIDs[n.ID],
			ParentID:             parent,
			Title:                n.Title,
			Body:                 remapIDs(n.Body, noteIDs, resourceIDs),
			CreatedTime:          orNow(n.CreatedTime, now),
			UpdatedTime:          orNow(n.UpdatedTime, now),
			IsConflict:           n.IsConflict,
			Latitude:             n.Latitude,
			Longitude:            n.Longitude,
			Altitude:             n.Altitude,
			Author:               n.Author,
			SourceURL:            n.SourceURL,
			IsTodo:               n.IsTodo,
			TodoDue:              n.TodoDue,
			TodoCompleted:        n.TodoCompleted,
			Source:               n.Source,
			SourceApplication:    n.SourceApplication,
			ApplicationData:      n.ApplicationData,
			Order:                n.Order,
			UserCreatedTime:      orNow(n.UserCreatedTime, now),
			UserUpdatedTime:      orNow(n.UserUpdatedTime, now),
			EncryptionCipherText: n.EncryptionCipherText,
			EncryptionApplied:    n.EncryptionApplied,
			MarkupLanguage:       orInt(n.MarkupLanguage, 1),
			IsShared:             n.IsShared,
			ShareID:              n.ShareID,
			ConflictOriginalID:   n.ConflictOriginalID,
			MasterKeyID:          n.MasterKeyID,
			UserData:             n.UserData,
		})
	}

	// Attachments: metadata now, bytes in a second pass. A resource whose
	// blob is still encrypted cannot be written as usable plaintext, so its
	// bytes are skipped with a warning rather than imported as ciphertext.
	encryptedBlobs := map[string]bool{}
	for _, r := range archive.Resources {
		if r.EncryptionBlobEncrypted != 0 {
			encryptedBlobs[resourceIDs[r.ID]] = true
		}
		batch.Resources = append(batch.Resources, &models.Resource{
			ID:                      resourceIDs[r.ID],
			Title:                   r.Title,
			Mime:                    r.Mime,
			Filename:                r.Filename,
			CreatedTime:             orNow(r.CreatedTime, now),
			UpdatedTime:             now,
			UserCreatedTime:         orNow(r.UserCreatedTime, now),
			UserUpdatedTime:         now,
			FileExtension:           r.FileExtension,
			EncryptionCipherText:    r.EncryptionCipherText,
			EncryptionApplied:       r.EncryptionApplied,
			EncryptionBlobEncrypted: r.EncryptionBlobEncrypted,
			Size:                    r.Size,
			IsShared:                r.IsShared,
			ShareID:                 r.ShareID,
			MasterKeyID:             r.MasterKeyID,
			UserData:                r.UserData,
			BlobUpdatedTime:         now,
		})
	}

	// Tags: reuse an existing tag with the same name, as Joplin does.
	for _, t := range archive.Tags {
		if existing, err := s.db.GetTagByTitle(t.Title); err != nil {
			return nil, err
		} else if existing != nil {
			tagIDs[t.ID] = existing.ID
			continue
		}
		newID := models.NewID()
		tagIDs[t.ID] = newID
		batch.Tags = append(batch.Tags, &models.Tag{
			ID:                   newID,
			Title:                t.Title,
			CreatedTime:          orNow(t.CreatedTime, now),
			UpdatedTime:          orNow(t.UpdatedTime, now),
			UserCreatedTime:      orNow(t.UserCreatedTime, now),
			UserUpdatedTime:      orNow(t.UserUpdatedTime, now),
			EncryptionCipherText: t.EncryptionCipherText,
			EncryptionApplied:    t.EncryptionApplied,
			IsShared:             t.IsShared,
			ParentID:             t.ParentID,
			UserData:             t.UserData,
		})
	}
	for _, nt := range archive.NoteTags {
		noteID, ok := noteIDs[nt.NoteID]
		if !ok {
			continue
		}
		tagID, ok := tagIDs[nt.TagID]
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("tag link %s references an unknown tag", nt.ID))
			continue
		}
		batch.NoteTags = append(batch.NoteTags, &models.NoteTag{
			ID:                   models.NewID(),
			NoteID:               noteID,
			TagID:                tagID,
			CreatedTime:          orNow(nt.CreatedTime, now),
			UpdatedTime:          orNow(nt.UpdatedTime, now),
			UserCreatedTime:      orNow(nt.UserCreatedTime, now),
			UserUpdatedTime:      orNow(nt.UserUpdatedTime, now),
			EncryptionCipherText: nt.EncryptionCipherText,
			EncryptionApplied:    nt.EncryptionApplied,
			IsShared:             nt.IsShared,
		})
	}

	res.Notes = len(batch.Notes)
	res.Folders = len(batch.Folders)
	res.Resources = len(batch.Resources)
	res.Tags = len(batch.Tags)
	res.NoteTags = len(batch.NoteTags)
	res.Notebook = fallbackID
	if destID != "" {
		res.Notebook = destID
	}

	if opts.DryRun {
		return res, nil
	}

	// Attachment bytes: written under their new ids before the batch lands, so
	// a failed write leaves no database rows pointing at nothing.
	var written []string
	sizes := map[string]int64{}
	cleanup := func() {
		for _, p := range written {
			os.Remove(p)
		}
	}
	f, err = os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reopen archive: %w", err)
	}
	err = jex.ExtractResources(f, func(name string, data io.Reader) error {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		newID, ok := resourceIDs[base]
		if !ok {
			return nil
		}
		if encryptedBlobs[newID] {
			res.Warnings = append(res.Warnings, fmt.Sprintf("attachment %s is encrypted; imported metadata only", base))
			return nil
		}
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if ext == "" {
			ext = "bin"
		}
		if strings.ContainsAny(ext, `/\`) || strings.Contains(ext, "..") {
			res.Warnings = append(res.Warnings, fmt.Sprintf("skipped attachment with unsafe extension %q", ext))
			return nil
		}
		dst := filepath.Join(s.db.ResourceDir(), newID+"."+ext)
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, data); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		if info, err := os.Stat(dst); err == nil {
			sizes[newID] = info.Size()
		}
		written = append(written, dst)
		return nil
	})
	f.Close()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("extract attachments: %w", err)
	}
	for _, r := range batch.Resources {
		if size, ok := sizes[r.ID]; ok {
			r.Size = size
		}
	}
	if len(written) < len(batch.Resources) {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("%d of %d attachments had no bytes in the archive", len(batch.Resources)-len(written), len(batch.Resources)))
	}

	if err := s.db.ImportBatch(batch); err != nil {
		cleanup()
		return nil, err
	}
	return res, nil
}

// remapIDs rewrites 32-hex ids in a note body that refer to imported items.
func remapIDs(body string, maps ...map[string]string) string {
	if body == "" {
		return body
	}
	return idPattern.ReplaceAllStringFunc(body, func(id string) string {
		for _, m := range maps {
			if newID, ok := m[id]; ok {
				return newID
			}
		}
		return id
	})
}

func (s *Service) uniqueFolderTitle(title, parentID string) (string, error) {
	if title == "" {
		title = "Untitled"
	}
	candidate := title
	for i := 1; ; i++ {
		taken, err := s.db.FolderTitleTaken(candidate, parentID)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s (%d)", title, i)
	}
}

func orNow(ms, now int64) int64 {
	if ms == 0 {
		return now
	}
	return ms
}

func orInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}
