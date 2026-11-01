package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// meshstackCmd groups the meshStack / STACKIT project provisioning steps.
var meshstackCmd = &cobra.Command{
	Use:   "meshstack",
	Short: "meshStack trial workspace, STACKIT project and service account",
	Long: `Steps after the account is enrolled (otp_enabled):

  create-project          project_assigned          meshStack trial sign-up (org = slug), records the STACKIT project UUID
  create-service-account  service_account_created   creates a service account + key in the project, grants it the project role, stores the key JSON
  grant-role              -                         (re-)grants the project role to an existing service account
  onboard                 -                         runs both
  sa-key                  -                         prints the stored service account key JSON`,
}

var meshCreateProjectCmd = &cobra.Command{
	Use:   "create-project <slug>",
	Short: "Step 'project_assigned': create the meshStack trial workspace and record the STACKIT project UUID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		if err := r.MeshProject(u, flagForce); err != nil {
			return err
		}
		fmt.Printf("%s: step %s, project %s\n", u.Slug, u.Step, u.Project.ID)
		return nil
	},
}

var meshCreateSACmd = &cobra.Command{
	Use:   "create-service-account <slug>",
	Short: "Step 'service_account_created': create a service account and download its key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		if err := r.ServiceAccount(u, flagForce); err != nil {
			return err
		}
		fmt.Printf("%s: step %s, service account %s, key %s\n", u.Slug, u.Step, u.Project.ServiceAccount.Email, r.KeyPath(u))
		return nil
	},
}

var meshOnboardCmd = &cobra.Command{
	Use:   "onboard <slug>...",
	Short: "Run the remaining meshStack steps (project, service account) for the given users",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		var failed []string
		for _, slug := range args {
			u, err := r.Store.Get(slug)
			if err != nil {
				return err
			}
			if err := r.MeshOnboard(u); err != nil {
				logf("meshstack onboard %s failed: %v", slug, err)
				failed = append(failed, slug)
				continue
			}
			fmt.Printf("%s: step %s\n", u.Slug, u.Step)
		}
		if len(failed) > 0 {
			return fmt.Errorf("meshstack onboarding failed for: %v", failed)
		}
		return nil
	},
}

var meshGrantRoleCmd = &cobra.Command{
	Use:   "grant-role <slug>...",
	Short: "Grant the service account the configured project role (meshstack.service_account_role, default owner)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		var failed []string
		for _, slug := range args {
			u, err := r.Store.Get(slug)
			if err != nil {
				return err
			}
			if err := r.GrantRole(u); err != nil {
				logf("grant-role %s failed: %v", slug, err)
				failed = append(failed, slug)
				continue
			}
			fmt.Printf("%s: %s has role %s\n", u.Slug, u.Project.ServiceAccount.Email, u.Project.ServiceAccount.Role)
		}
		if len(failed) > 0 {
			return fmt.Errorf("grant-role failed for: %v", failed)
		}
		return nil
	},
}

var meshSAKeyCmd = &cobra.Command{
	Use:   "sa-key <slug>",
	Short: "Print the service account key JSON of a user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		path := r.KeyPath(u)
		if path == "" {
			return fmt.Errorf("user %s has no service account key (step %q)", u.Slug, u.Step)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	},
}

func init() {
	for _, c := range []*cobra.Command{meshCreateProjectCmd, meshCreateSACmd} {
		c.Flags().BoolVar(&flagForce, "force", false, "run even if the user is not at the expected previous step")
	}
	meshstackCmd.AddCommand(meshCreateProjectCmd, meshCreateSACmd, meshGrantRoleCmd, meshOnboardCmd, meshSAKeyCmd)
	rootCmd.AddCommand(meshstackCmd)
}

var flagWatchDuration time.Duration

var meshWatchCmd = &cobra.Command{
	Use:    "watch <slug>",
	Short:  "Debug: log in to meshStack as the user and report page changes without clicking anything",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		return r.MeshWatch(u, flagWatchDuration)
	},
}

func init() {
	meshWatchCmd.Flags().DurationVar(&flagWatchDuration, "duration", 2*time.Minute, "how long to observe")
	meshstackCmd.AddCommand(meshWatchCmd)
}

var meshInspectAccessCmd = &cobra.Command{
	Use:    "inspect-access <slug>",
	Short:  "Debug: show the IAM access page of the user's project (who has which role)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		txt, err := r.MeshInspectAccess(u)
		if err != nil {
			return err
		}
		fmt.Println(txt)
		return nil
	},
}

func init() { meshstackCmd.AddCommand(meshInspectAccessCmd) }

var meshInspectGrantCmd = &cobra.Command{
	Use:    "inspect-grant <slug>",
	Short:  "Debug: dump the 'Zugriff gewähren' dialog of the project's access page",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		txt, err := r.MeshInspectGrant(u)
		if err != nil {
			return err
		}
		fmt.Println(txt)
		return nil
	},
}

func init() { meshstackCmd.AddCommand(meshInspectGrantCmd) }
