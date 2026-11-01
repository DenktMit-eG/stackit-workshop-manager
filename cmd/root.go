// Package cmd contains the CLI commands; every account-creation step is its own command.
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/denktmit/stackit-ws-manager/internal/config"
	"github.com/denktmit/stackit-ws-manager/internal/steps"
	"github.com/denktmit/stackit-ws-manager/internal/store"
)

var (
	flagConfig  string
	flagHeadful bool
	flagForce   bool
)

var rootCmd = &cobra.Command{
	Use:           "stackit-ws-manager",
	Short:         "Create and manage STACKIT workshop accounts",
	Long:          `stackit-ws-manager automates the multi-step STACKIT account registration for workshop participants and keeps an ad-hoc password/OTP directory.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI.
func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "config.yaml", "path to the configuration file")
	rootCmd.PersistentFlags().BoolVar(&flagHeadful, "headful", false, "run the browser with a visible window (debugging)")
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// runner loads config + store and builds the step runner.
func runner() (*steps.Runner, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, err
	}
	st, err := store.Load(cfg.UsersFile)
	if err != nil {
		return nil, err
	}
	r := &steps.Runner{Cfg: cfg, Store: st, Log: logf}
	if flagHeadful {
		f := false
		r.Headless = &f
	}
	return r, nil
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a default config.yaml",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat(flagConfig); err == nil && !flagForce {
			return fmt.Errorf("%s exists (use --force to overwrite)", flagConfig)
		}
		if err := config.Write(flagConfig, config.Default()); err != nil {
			return err
		}
		fmt.Println("wrote", flagConfig)
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&flagForce, "force", false, "overwrite an existing file")
	rootCmd.AddCommand(initCmd)
}
