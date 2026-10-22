package jex

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// ExportData is the item set to write. ResourceDir is the profile's resources
// folder, where attachment bytes are read from.
type ExportData struct {
	Notes       []*models.Note
	Folders     []*models.Folder
	Resources   []*models.Resource
	Tags        []*models.Tag
	NoteTags    []*models.NoteTag
	ResourceDir string
	// Progress is called after each archive entry; total is known up front.
	Progress func(done, total int, name string)
}

// ExportStats reports what an export wrote.
type ExportStats struct {
	Notes     int
	Folders   int
	Resources int
	Tags      int
	NoteTags  int
	Bytes     int64
	Warnings  []string
}

// Export streams the archive to w. Attachment bytes are copied straight from
// disk, so memory use stays flat regardless of library size.
func Export(w io.Writer, d ExportData) (ExportStats, error) {
	var stats ExportStats
	tw := tar.NewWriter(w)
	defer tw.Close()

	total := len(d.Folders) + len(d.Notes) + len(d.Resources) + len(d.Tags) + len(d.NoteTags)
	done := 0
	step := func(name string) {
		done++
		if d.Progress != nil {
			d.Progress(done, total, name)
		}
	}

	writeItem := func(id, content string, modTime time.Time) error {
		data := []byte(content)
		if err := tw.WriteHeader(&tar.Header{
			Name:     id + ".md",
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  modTime,
			Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		stats.Bytes += int64(len(data))
		return nil
	}

	for _, f := range d.Folders {
		if err := writeItem(f.ID, SerializeFolder(f), modTime(f.UpdatedTime)); err != nil {
			return stats, fmt.Errorf("write notebook %s: %w", f.ID, err)
		}
		step(f.ID + ".md")
	}
	stats.Folders = len(d.Folders)

	for _, n := range d.Notes {
		if err := writeItem(n.ID, SerializeNote(n), modTime(n.UpdatedTime)); err != nil {
			return stats, fmt.Errorf("write note %s: %w", n.ID, err)
		}
		step(n.ID + ".md")
	}
	stats.Notes = len(d.Notes)

	for _, r := range d.Resources {
		if err := writeItem(r.ID, SerializeResource(r), modTime(r.UpdatedTime)); err != nil {
			return stats, fmt.Errorf("write resource %s: %w", r.ID, err)
		}
		src, ext, err := resourceSource(d.ResourceDir, r)
		if err != nil {
			return stats, err
		}
		if src == "" {
			stats.Warnings = append(stats.Warnings,
				fmt.Sprintf("attachment %s (%s) has no local file; exported metadata only", r.ID, r.Title))
			step(r.ID + ".md")
			continue
		}
		if err := appendFile(tw, "resources/"+r.ID+"."+ext, src, modTime(r.UpdatedTime)); err != nil {
			return stats, fmt.Errorf("write attachment %s: %w", r.ID, err)
		}
		info, err := os.Stat(src)
		if err == nil {
			stats.Bytes += info.Size()
		}
		step(r.ID + "." + ext)
	}
	stats.Resources = len(d.Resources)

	for _, t := range d.Tags {
		if err := writeItem(t.ID, SerializeTag(t), modTime(t.UpdatedTime)); err != nil {
			return stats, fmt.Errorf("write tag %s: %w", t.ID, err)
		}
		step(t.ID + ".md")
	}
	stats.Tags = len(d.Tags)

	for _, nt := range d.NoteTags {
		if err := writeItem(nt.ID, SerializeNoteTag(nt), modTime(nt.UpdatedTime)); err != nil {
			return stats, fmt.Errorf("write note tag %s: %w", nt.ID, err)
		}
		step(nt.ID + ".md")
	}
	stats.NoteTags = len(d.NoteTags)

	if err := tw.Close(); err != nil {
		return stats, fmt.Errorf("finish archive: %w", err)
	}
	return stats, nil
}

// resourceSource finds the attachment bytes for r. Official Joplin profiles
// name files <id>.<extension>; Lite's own profiles may use a bare <id>.
func resourceSource(dir string, r *models.Resource) (path, ext string, err error) {
	if dir == "" {
		return "", "", nil
	}
	if r.EncryptionBlobEncrypted != 0 {
		return "", "", nil
	}
	if ext := r.FileExtension; ext != "" && !strings.ContainsAny(ext, `/\`) {
		p := filepath.Join(dir, r.ID+"."+ext)
		if _, err := os.Stat(p); err == nil {
			return p, ext, nil
		}
	}
	p := filepath.Join(dir, r.ID)
	if _, err := os.Stat(p); err == nil {
		return p, strings.TrimPrefix(r.FileExtension, "."), nil
	}
	return "", "", nil
}

func appendFile(tw *tar.Writer, name, src string, modTime time.Time) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     info.Size(),
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func modTime(ms int64) time.Time {
	if ms == 0 {
		return time.Now()
	}
	return time.UnixMilli(ms)
}
