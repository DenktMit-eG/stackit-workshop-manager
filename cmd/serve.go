package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/denktmit/stackit-ws-manager/internal/mailhog"
	"github.com/denktmit/stackit-ws-manager/internal/mailstore"
	"github.com/denktmit/stackit-ws-manager/internal/web"
)

var flagListen string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the web UI (password manager view of users.yaml, actions, mail archive)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		cfg := r.Cfg
		if flagListen != "" {
			cfg.Web.Listen = flagListen
		}
		srv, err := web.New(cfg, r.Headless)
		if err != nil {
			return err
		}
		ctx, cancel := signalContext()
		defer cancel()
		return srv.Run(ctx)
	},
}

var mailSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Copy all mails of all users from MailHog into the local mail archive",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		ctx, cancel := signalContext()
		defer cancel()
		ms := &mailstore.Store{Dir: r.Cfg.Web.MailDir}
		added, err := ms.Sync(ctx, mailhog.New(r.Cfg.MailhogURL), r.Store.Dir.Users)
		for slug, n := range added {
			fmt.Printf("%s: %d new mail(s)\n", slug, n)
		}
		return err
	},
}

func init() {
	serveCmd.Flags().StringVar(&flagListen, "listen", "", "listen address (overrides config web.listen)")
	mailCmd.AddCommand(mailSyncCmd)
	rootCmd.AddCommand(serveCmd)
}
