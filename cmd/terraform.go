package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var terraformCmd = &cobra.Command{
	Use:   "terraform",
	Short: "Per-user terraform landing zone: deploy, zip, destroy",
	Long: `Step after the service account exists (service_account_created):

  deploy   terraform_deployed   copy the template, install the SA key (+ extracted PEM), write
                                teilnehmer.auto.tfvars, enable object storage via API, terraform init + apply,
                                write output/project.env, zip the project to <work_dir>/<slug>.zip
  enable-s3  -                  only the object storage enablement (API call with the SA key)
  zip      -                    re-create the zip from the current project directory
  outputs  -                    print output/project.env
  destroy  -                    terraform destroy (keeps the directory and zip)`,
}

var tfDeployCmd = &cobra.Command{
	Use:   "deploy <slug>...",
	Short: "Step 'terraform_deployed': apply the landing zone in the user's project and zip it",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		ctx, cancel := signalContext()
		defer cancel()
		var failed []string
		for _, slug := range args {
			u, err := r.Store.Get(slug)
			if err != nil {
				return err
			}
			if err := r.TerraformDeploy(ctx, u, flagForce); err != nil {
				logf("terraform deploy %s failed: %v", slug, err)
				failed = append(failed, slug)
				continue
			}
			fmt.Printf("%s: step %s, zip %s\n", u.Slug, u.Step, r.TerraformZipPath(u))
		}
		if len(failed) > 0 {
			return fmt.Errorf("terraform deploy failed for: %v", failed)
		}
		return nil
	},
}

var tfEnableS3Cmd = &cobra.Command{
	Use:   "enable-s3 <slug>...",
	Short: "Enable the Object Storage service in the user's project via the API (done automatically by deploy)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		ctx, cancel := signalContext()
		defer cancel()
		for _, slug := range args {
			u, err := r.Store.Get(slug)
			if err != nil {
				return err
			}
			if err := r.EnableObjectStorage(ctx, u); err != nil {
				return err
			}
		}
		return nil
	},
}

var tfZipCmd = &cobra.Command{
	Use:   "zip <slug>",
	Short: "Re-create the downloadable zip of the user's terraform project",
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
		if err := r.TerraformZip(u); err != nil {
			return err
		}
		fmt.Println(r.TerraformZipPath(u))
		return nil
	},
}

var tfOutputsCmd = &cobra.Command{
	Use:   "outputs <slug>",
	Short: "Print the flattened terraform outputs (output/project.env)",
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
		out := r.TerraformOutputs(u)
		if out == "" {
			return fmt.Errorf("no outputs for %s (not deployed yet?)", u.Slug)
		}
		fmt.Print(out)
		return nil
	},
}

var tfDestroyCmd = &cobra.Command{
	Use:   "destroy <slug>",
	Short: "terraform destroy the user's landing zone",
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
		ctx, cancel := signalContext()
		defer cancel()
		return r.TerraformDestroy(ctx, u)
	},
}

func init() {
	tfDeployCmd.Flags().BoolVar(&flagForce, "force", false, "run even if the user is not at the expected previous step")
	terraformCmd.AddCommand(tfDeployCmd, tfEnableS3Cmd, tfZipCmd, tfOutputsCmd, tfDestroyCmd)
	rootCmd.AddCommand(terraformCmd)
}
