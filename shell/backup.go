package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/service"
)

// backupStatus is the one-line state shown under the workspace while an
// export/import runs or after it finishes.
func (a *nativeApp) backupStatus() (string, ui.Color) {
	switch {
	case a.backupErr != "":
		return a.backupErr, signalRed
	case a.backupBusy != "":
		return a.backupBusy, inkSoft
	case a.backupNotice != "":
		return a.backupNotice, inkSoft
	}
	return "", inkSoft
}

// exportAll asks for a destination and writes the whole library there. The
// archive is built in the background; the footer reports progress.
func (a *nativeApp) exportAll() {
	if a.backupBusy != "" {
		return
	}
	a.flush()
	a.backupErr, a.backupNotice = "", ""
	a.backupBusy = "Preparing export…"
	a.invalidateNow()

	go func() {
		path, err := a.chooseExportPath()
		if err != nil {
			a.finishBackup("", "Could not export: "+err.Error())
			return
		}
		if path == "" {
			a.finishBackup("", "")
			return
		}
		path = withJEXExtension(path)
		a.setBackupBusy("Exporting notes…")
		stats, err := a.svc.ExportJEX(path, func(done, total int, _ string) {
			if done%200 != 0 && done != total {
				return
			}
			a.setBackupBusy(fmt.Sprintf("Exporting notes… %d/%d", done, total))
		})
		if err != nil {
			a.finishBackup("", "Could not export: "+err.Error())
			return
		}
		note := fmt.Sprintf("Exported %d notes, %d notebooks and %d attachments to %s",
			stats.Notes, stats.Folders, stats.Resources, filepath.Base(path))
		if len(stats.Warnings) > 0 {
			note += fmt.Sprintf(" (%d warnings)", len(stats.Warnings))
		}
		a.finishBackup(note, "")
	}()
}

// chooseImportArchive picks an archive, validates it with a dry run, then
// asks for confirmation before writing anything.
func (a *nativeApp) chooseImportArchive() {
	if a.backupBusy != "" || a.confirmImport {
		return
	}
	a.backupErr, a.backupNotice = "", ""
	a.backupBusy = "Choosing archive…"
	a.invalidateNow()

	go func() {
		path, err := a.chooseImportFile()
		if err != nil {
			a.finishBackup("", "Could not read archive: "+err.Error())
			return
		}
		if path == "" {
			a.finishBackup("", "")
			return
		}
		a.setBackupBusy("Reading archive…")
		plan, err := a.svc.ImportJEX(path, service.ImportOptions{DryRun: true})
		mygo.RunOnMain(func() {
			a.backupBusy = ""
			if err != nil {
				a.backupErr = "Could not read archive: " + err.Error()
			} else if plan.Notes == 0 && plan.Folders == 0 && plan.Resources == 0 {
				a.backupErr = "That archive contains no notes."
			} else {
				a.importPath = path
				a.importPlan = plan
				a.confirmImport = true
			}
			a.invalidateNow()
		})
	}()
}

// startImport performs the import the confirmation dialog approved.
func (a *nativeApp) startImport() {
	path := a.importPath
	a.importPath, a.importPlan = "", nil
	if path == "" {
		return
	}
	a.backupErr, a.backupNotice = "", ""
	a.backupBusy = "Importing notes…"
	a.invalidateNow()

	go func() {
		result, err := a.svc.ImportJEX(path, service.ImportOptions{})
		mygo.RunOnMain(func() {
			a.backupBusy = ""
			if err != nil {
				a.backupErr = "Could not import: " + err.Error()
			} else {
				a.backupNotice = fmt.Sprintf("Imported %d notes, %d notebooks and %d attachments",
					result.Notes, result.Folders, result.Resources)
				if len(result.Warnings) > 0 {
					a.backupNotice += fmt.Sprintf(" (%d warnings)", len(result.Warnings))
				}
				a.reloadNotebooks()
				a.reloadTags()
				a.reloadNotes()
				a.reloadNoteTags()
			}
			a.invalidateNow()
		})
	}()
}

// backupDialogs renders the import confirmation. Call it with the workspace's
// other modal dialogs.
func (a *nativeApp) backupDialogs(c *ui.Context, t *ui.Theme) {
	if !a.confirmImport || a.importPlan == nil {
		return
	}
	p := a.importPlan
	message := fmt.Sprintf("%d notes, %d notebooks and %d attachments from \"%s\".\n\nEvery item gets a new id, so nothing is overwritten; importing the same archive twice adds a second copy.",
		p.Notes, p.Folders, p.Resources, filepath.Base(a.importPath))
	if confirmDialogKeys(c, actionTheme, &a.confirmImport, "Import archive?", message, "Import", true) == 1 {
		a.startImport()
	} else if !a.confirmImport {
		a.importPath, a.importPlan = "", nil
	}
}

// chooseExportPath uses the native save panel, or the test seam when set.
func (a *nativeApp) chooseExportPath() (string, error) {
	if a.pickExportPath != nil {
		return a.pickExportPath()
	}
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:       "Export all notes",
		DefaultPath: defaultExportPath(a.db.DataDir()),
		ButtonLabel: "Export",
		Filters:     []mygo.FileFilter{{Name: "JEX archive", Extensions: []string{"jex"}}},
	})
}

func (a *nativeApp) chooseImportFile() (string, error) {
	if a.pickImportPath != nil {
		return a.pickImportPath()
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:   "Import JEX archive",
		Filters: []mygo.FileFilter{{Name: "JEX archive", Extensions: []string{"jex"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// defaultExportPath suggests a dated file beside the user's other backups.
func defaultExportPath(dataDir string) string {
	name := strings.TrimSuffix(filepath.Base(dataDir), filepath.Ext(dataDir))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "all_notebooks"
	}
	file := fmt.Sprintf("%s-%s.jex", name, time.Now().Format("20060102-1504"))
	home, err := os.UserHomeDir()
	if err != nil {
		return file
	}
	backups := filepath.Join(home, "JoplinBackup")
	if info, err := os.Stat(backups); err == nil && info.IsDir() {
		return filepath.Join(backups, file)
	}
	return filepath.Join(home, file)
}

// withJEXExtension keeps exactly one .jex on the chosen path. The save panel
// is handed a default name that already ends in .jex but still appends its
// filter's extension, which would write backup.jex.jex; a name typed without
// an extension needs the opposite treatment.
func withJEXExtension(path string) string {
	if path == "" {
		return path
	}
	for strings.HasSuffix(strings.ToLower(path), ".jex.jex") {
		path = path[:len(path)-len(".jex")]
	}
	if !strings.HasSuffix(strings.ToLower(path), ".jex") {
		path += ".jex"
	}
	return path
}

func (a *nativeApp) setBackupBusy(text string) {
	mygo.RunOnMain(func() {
		a.backupBusy = text
		a.invalidateNow()
	})
}

func (a *nativeApp) finishBackup(notice, errText string) {
	mygo.RunOnMain(func() {
		a.backupBusy = ""
		a.backupNotice = notice
		a.backupErr = errText
		a.invalidateNow()
	})
}

func (a *nativeApp) invalidateNow() {
	if a.invalidate != nil {
		a.invalidate()
	}
}
