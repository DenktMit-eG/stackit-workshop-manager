package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/denktmit/stackit-ws-manager/internal/browser"
)

var browserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Playwright browser maintenance",
}

var flagWithDeps bool

var browserInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Download the Playwright driver and Chromium matching this build",
	Long: `Downloads the Playwright driver (node + playwright-core) and Chromium into ~/.cache.
With --with-deps the system libraries Chromium needs are installed too (runs apt via sudo on
Debian/Ubuntu; see scripts/install.sh for the full host setup).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return browser.Install(flagWithDeps)
	},
}

var browserCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Launch headless Chromium once to verify the installation",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		version, err := browser.Check()
		if err != nil {
			return err
		}
		fmt.Println("headless chromium ok:", version)
		return nil
	},
}

func init() {
	browserCmd.AddCommand(browserCheckCmd)
	browserInstallCmd.Flags().BoolVar(&flagWithDeps, "with-deps", false, "also install the system libraries Chromium needs (sudo apt)")
	browserCmd.AddCommand(browserInstallCmd)
	rootCmd.AddCommand(browserCmd)
}
