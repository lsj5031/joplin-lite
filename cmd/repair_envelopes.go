package cmd

import (
	"fmt"

	"github.com/lsj5031/joplin-lite/internal/enveloperepair"
	"github.com/spf13/cobra"
)

func newRepairEnvelopesCmd() *cobra.Command {
	var apply bool
	var backup, rollback string
	cmd := &cobra.Command{
		Use:   "repair-envelopes <absolute-exported-sync-directory>",
		Short: "Repair encrypted metadata in an offline sync-target copy (dry-run by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if backup != "" && rollback != "" {
				return fmt.Errorf("--backup and --rollback are mutually exclusive")
			}
			entries, err := enveloperepair.Run(args[0], backup, rollback, apply)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				fmt.Fprintln(cmd.OutOrStdout(), entry.ID)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "affected: %d\n", len(entries))
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write changes, only to an exclusively owned offline copy")
	cmd.Flags().StringVar(&backup, "backup", "", "new absolute backup directory outside the export, required for apply")
	cmd.Flags().StringVar(&rollback, "rollback", "", "restore a previous backup (also dry-run unless --apply)")
	return cmd
}

func init() { rootCmd.AddCommand(newRepairEnvelopesCmd()) }
