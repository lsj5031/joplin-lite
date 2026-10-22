package cmd

import (
	"fmt"

	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/service"
	"github.com/spf13/cobra"
)

var (
	importNotebook string
	importDryRun   bool
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import notes from another format",
}

var importJexCmd = &cobra.Command{
	Use:   "jex <file.jex>",
	Short: "Import notes, notebooks and attachments from a JEX archive",
	Long: "Import a JEX archive. Items keep their titles and content but receive\n" +
		"new ids, so importing the same archive twice creates two copies.\n" +
		"Notebooks are recreated from the archive unless --notebook is given,\n" +
		"in which case every note lands in that existing notebook.\n" +
		"Use --dry-run first to see what would be imported.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadLocalConfig()
		if err != nil {
			return err
		}
		db, err := store.Open(cfg.DataDir)
		if err != nil {
			return err
		}
		defer db.Close()

		result, err := service.New(db).ImportJEX(args[0], service.ImportOptions{
			DryRun:     importDryRun,
			NotebookID: importNotebook,
		})
		if err != nil {
			return err
		}

		verb := "imported"
		if importDryRun {
			verb = "would import"
		}
		fmt.Printf("%s %d notes, %d notebooks, %d attachments, %d tags, %d tag links\n",
			verb, result.Notes, result.Folders, result.Resources, result.Tags, result.NoteTags)
		if importDryRun {
			fmt.Println("dry run: nothing was written")
		}
		for _, w := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
		}
		return nil
	},
}

func init() {
	importJexCmd.Flags().StringVar(&importNotebook, "notebook", "", "import all notes into this notebook id instead of recreating the tree")
	importJexCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "parse and validate without writing anything")
	importJexCmd.Flags().StringVar(&backupDataDir, "data-dir", "", "profile directory to import into (default: from config)")
	importCmd.AddCommand(importJexCmd)
	rootCmd.AddCommand(importCmd)
}
