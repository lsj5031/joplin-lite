package cmd

import (
	"fmt"
	"os"

	"github.com/lsj5031/joplin-lite/internal/sync"
	"github.com/spf13/cobra"
)

var dropboxAuthRedirect string

var dropboxAuthCmd = &cobra.Command{
	Use:   "dropbox-auth",
	Short: "Authorize this client with your own Dropbox OAuth app (sync target 7)",
	Long: `Runs the Dropbox OAuth2 authorization-code flow with PKCE against your own
Dropbox OAuth app (scoped, "App folder" permission). The authorization URL is
printed; after you approve access in the browser the token is stored under the
data dir (dropbox_token.json) and refreshed automatically on sync.

The redirect URI must be registered in the Dropbox app console (default:
http://127.0.0.1:41723).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if cfg.DropboxAppKey == "" {
			return fmt.Errorf("no Dropbox app key configured (sync.7.appKey or GOJOPLIN_DROPBOX_APP_KEY)")
		}
		oauth := sync.NewDropboxOAuth(cfg.DropboxAppKey, cfg.DropboxAppSecret)
		if dropboxAuthRedirect != "" {
			oauth.Redirect = dropboxAuthRedirect
		}
		tok, err := sync.RunDropboxOAuthFlow(cmd.Context(), oauth, os.Stdout)
		if err != nil {
			return fmt.Errorf("Dropbox authorization failed: %w", err)
		}
		if err := sync.SaveDropboxToken(cfg.DataDir, tok); err != nil {
			return fmt.Errorf("cannot store Dropbox token: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Dropbox authorized (account %s). Token stored in %s\n",
			tok.AccountID, sync.DropboxTokenPath(cfg.DataDir))
		return nil
	},
}

func init() {
	dropboxAuthCmd.Flags().StringVar(&dropboxAuthRedirect, "redirect", "", "loopback redirect URI (must match the Dropbox app registration)")
	rootCmd.AddCommand(dropboxAuthCmd)
}
