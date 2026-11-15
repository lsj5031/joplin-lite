package cmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/dropboxrepair"
	jsync "github.com/lsj5031/joplin-lite/internal/sync"
	"github.com/spf13/cobra"
)

func newRepairDropboxCmd() *cobra.Command {
	var profile, backup, scan string
	var apply, rollback bool
	cmd := &cobra.Command{
		Use:   "repair-dropbox-envelopes",
		Short: "Prepare private backups, or apply a reviewed Dropbox envelope manifest using CAS",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !filepath.IsAbs(profile) || !filepath.IsAbs(backup) {
				return errors.New("--official-profile and --backup must be absolute")
			}
			// Do not load environment overrides, another account's token cache,
			// or the app's normal sync engine. Only this explicitly selected
			// official profile's existing Dropbox authorization is permitted.
			raw, err := os.ReadFile(filepath.Join(profile, "settings.json"))
			if err != nil {
				return err
			}
			var settings struct {
				Target int `json:"sync.target"`
			}
			if json.Unmarshal(raw, &settings) != nil || settings.Target != 7 {
				return errors.New("selected official settings must use Dropbox target 7")
			}
			auth, err := config.OfficialDropboxAuth(profile)
			if err != nil {
				return err
			}
			token, err := jsync.LoadDropboxToken("", auth)
			if err != nil || token == nil || token.AccessToken == "" {
				return errors.New("cannot recover existing official Dropbox authorization")
			}
			client := dropboxrepair.NewClient(token.AccessToken)
			client.Progress = func(stage string, count int) {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: %d\n", stage, count)
			}
			if apply && scan != "" {
				return errors.New("--verify-scan cannot be combined with --apply")
			}
			if apply {
				n, err := dropboxrepair.Apply(cmd.Context(), client, backup, rollback)
				fmt.Fprintf(cmd.OutOrStdout(), "verified journal entries: %d\n", n)
				return err
			}
			if rollback {
				return errors.New("--rollback requires --apply; it is guarded by the recorded success revisions")
			}
			u := url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(profile, "database.sqlite")), RawQuery: "mode=ro"}
			db, err := sql.Open("sqlite", u.String())
			if err != nil {
				return errors.New("cannot read official master key IDs")
			}
			defer db.Close()
			rows, err := db.Query("SELECT id FROM master_keys ORDER BY id")
			if err != nil {
				return errors.New("cannot read official master key IDs")
			}
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if scan != "" {
				v, err := dropboxrepair.Verify(cmd.Context(), client, backup, scan, ids)
				if v != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "verified: %d\nremaining affected: %d\nunrelated inventory changes: %d\n", v.Verified, v.Affected, len(v.Unrelated))
				}
				return err
			}
			m, err := dropboxrepair.Prepare(cmd.Context(), client, backup, ids)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "identity: %s\nmaster key IDs match official profile: %d\ninventory paths: %d\naffected: %d\nbackup: %s\n", m.Identity, len(m.MasterKeys), len(m.Inventory), len(m.Entries), backup)
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "official-profile", "", "explicit existing official Joplin profile (read-only)")
	cmd.Flags().StringVar(&backup, "backup", "", "new private backup for preparation, or existing reviewed backup for apply")
	cmd.Flags().BoolVar(&apply, "apply", false, "conditionally upload only the existing manifest's affected items")
	cmd.Flags().BoolVar(&rollback, "rollback", false, "restore exact originals conditional on recorded post-repair revisions")
	cmd.Flags().StringVar(&scan, "verify-scan", "", "verify without writes and save stable rescan/evidence in a new absolute directory")
	return cmd
}

func init() { rootCmd.AddCommand(newRepairDropboxCmd()) }
