package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/denktmit/stackit-ws-manager/internal/mailhog"
	"github.com/denktmit/stackit-ws-manager/internal/mailstore"
)

var mailCmd = &cobra.Command{
	Use:   "mail",
	Short: "Inspect the MailHog mail sink",
}

var mailListCmd = &cobra.Command{
	Use:   "list [slug]",
	Short: "List mails in MailHog, optionally only those addressed to a user",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		mh := mailhog.New(r.Cfg.MailhogURL)
		ctx, cancel := signalContext()
		defer cancel()
		var msgs []mailhog.Message
		if len(args) == 1 {
			u, err := r.Store.Get(args[0])
			if err != nil {
				return err
			}
			msgs, err = mh.SearchTo(ctx, u.Username, 50)
			if err != nil {
				return err
			}
		} else {
			msgs, err = mh.Messages(ctx, 50)
			if err != nil {
				return err
			}
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CREATED\tFROM\tTO\tSUBJECT")
		for _, m := range msgs {
			to := ""
			for i, t := range m.To {
				if i > 0 {
					to += ","
				}
				to += t.String()
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", m.Created.Local().Format(time.RFC3339), m.FromAddress(), to, m.Subject())
		}
		return w.Flush()
	},
}

var mailDedupeCmd = &cobra.Command{
	Use:   "dedupe",
	Short: "Remove duplicate files from the local mail archive (same MailHog message archived more than once)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		ms := &mailstore.Store{Dir: r.Cfg.Web.MailDir}
		removed, err := ms.Dedupe()
		for slug, n := range removed {
			fmt.Printf("%s: removed %d duplicate(s)\n", slug, n)
		}
		return err
	},
}

func init() {
	mailCmd.AddCommand(mailListCmd, mailDedupeCmd)
	rootCmd.AddCommand(mailCmd)
}
