package cmd

import (
	"fmt"
	"os"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/service"
	"github.com/spf13/cobra"
)

var (
	exportForce bool
	// backupDataDir overrides the profile these two commands work on, so an
	// archive can be produced from any profile without a config file rewrite.
	backupDataDir string
)

// loadLocalConfig loads the config for local-only work. Export and import
// never sync, so an unconfigured (local-only) profile is accepted.
func loadLocalConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgFile, config.Overrides{
		Username:       username,
		Password:       password,
		APIKey:         apiKey,
		MasterPassword: masterPassword,
		// These commands never sync, and requiring sync credentials just to
		// read the local database would block exports on misconfigured hosts.
		NoSync:     true,
		AllowLocal: true,
	})
	if err != nil {
		return nil, err
	}
	if backupDataDir != "" {
		cfg.DataDir = backupDataDir
	}
	return cfg, nil
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the library",
}

var exportJexCmd = &cobra.Command{
	Use:   "jex <file.jex>",
	Short: "Export notes, notebooks and attachments to a JEX archive",
	Long: "Write the whole library to a JEX archive (Joplin's lossless export\n" +
		"format: an uncompressed tar of item files plus a resources/ directory).\n" +
		"The archive is plain text, including note bodies and attachment bytes.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		if !exportForce {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists; pass --force to overwrite", path)
			}
		}
		cfg, err := loadLocalConfig()
		if err != nil {
			return err
		}
		db, err := store.Open(cfg.DataDir)
		if err != nil {
			return err
		}
		defer db.Close()

		stats, err := service.New(db).ExportJEX(path, nil)
		if err != nil {
			return err
		}

		info, _ := os.Stat(path)
		fmt.Printf("exported %d notes, %d notebooks, %d attachments, %d tags, %d tag links\n",
			stats.Notes, stats.Folders, stats.Resources, stats.Tags, stats.NoteTags)
		if info != nil {
			fmt.Printf("wrote %s (%s)\n", path, humanBytes(info.Size()))
		} else {
			fmt.Printf("wrote %s\n", path)
		}
		for _, w := range stats.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		return nil
	},
}

// humanBytes renders a size for the CLI summary.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func init() {
	exportJexCmd.Flags().BoolVar(&exportForce, "force", false, "overwrite the target file if it exists")
	exportJexCmd.Flags().StringVar(&backupDataDir, "data-dir", "", "profile directory to export (default: from config)")
	exportCmd.AddCommand(exportJexCmd)
	rootCmd.AddCommand(exportCmd)
}
