package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/denktmit/stackit-ws-manager/internal/browser"
	"github.com/denktmit/stackit-ws-manager/internal/config"
	"github.com/denktmit/stackit-ws-manager/internal/store"
	"github.com/denktmit/stackit-ws-manager/internal/totp"
)

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// stackitAccCmd groups everything around STACKIT account registration.
var stackitAccCmd = &cobra.Command{
	Use:   "stackit-acc",
	Short: "STACKIT account registration: user directory and the registration steps",
	Long: `Account creation steps (each is its own command, run them in order):

  user-add      created         choose slug, generate password
  register      registered      submit the sign-up form (Playwright)
  verify-email  email_verified  wait for the mails in MailHog, call the verification link
  setup-otp     otp_enabled     log in, enrol TOTP, store the secret (Playwright)
  otp           -               print the current OTP code for a user
  onboard       -               run all remaining registration steps for a user`,
}

var (
	flagName  string
	flagEmail string
	flagQuiet bool
)

var userAddCmd = &cobra.Command{
	Use:   "user-add <slug>",
	Short: "Step 'created': register a new user slug and generate its password",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Create(args[0], store.Participant{Name: flagName, Email: flagEmail})
		if err != nil {
			return err
		}
		printUser(u, true)
		return nil
	},
}

var userAssignCmd = &cobra.Command{
	Use:   "user-assign <slug>",
	Short: "Assign a real participant (name/e-mail) to a user",
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
		u.Participant = store.Participant{Name: flagName, Email: flagEmail}
		u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		if err := r.Store.Save(u); err != nil {
			return err
		}
		printUser(u, false)
		return nil
	},
}

var userListCmd = &cobra.Command{
	Use:   "user-list",
	Short: "List all users and their current step",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SLUG\tUSERNAME\tSTEP\tPARTICIPANT\tPROJECT\tSERVICE ACCOUNT")
		for _, u := range r.Store.Dir.Users {
			p := u.Participant.Name
			if u.Participant.Email != "" {
				p += " <" + u.Participant.Email + ">"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", u.Slug, u.Username, u.Step, p, u.Project.ID, u.Project.ServiceAccount.Email)
		}
		return w.Flush()
	},
}

var userShowCmd = &cobra.Command{
	Use:   "user-show <slug>",
	Short: "Show one user including password, current OTP, recovery codes and service account",
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
		printUser(u, true)
		return nil
	},
}

var userRemoveCmd = &cobra.Command{
	Use:   "user-remove <slug>",
	Short: "Remove a user from the directory (does not touch STACKIT)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		if err := r.Store.Remove(args[0]); err != nil {
			return err
		}
		return r.Store.Save()
	},
}

var userSetProjectCmd = &cobra.Command{
	Use:   "user-set-project <slug> <project-uuid>",
	Short: "Step 'project_assigned': record the STACKIT project UUID manually",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		u.Project.ID = args[1]
		u.Advance(store.StepProjectAssigned)
		if err := r.Store.Save(u); err != nil {
			return err
		}
		printUser(u, false)
		return nil
	},
}

var userSetStepCmd = &cobra.Command{
	Use:   "user-set-step <slug> <step>",
	Short: "Manually set the registration step of a user (repair tool)",
	Long:  "Valid steps: created, registered, email_verified, otp_enabled, project_assigned, service_account_created",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := runner()
		if err != nil {
			return err
		}
		u, err := r.Store.Get(args[0])
		if err != nil {
			return err
		}
		step := store.Step(args[1])
		if step.Index() < 0 {
			return fmt.Errorf("unknown step %q", args[1])
		}
		u.Step = step
		u.Advance(step)
		if err := r.Store.Save(u); err != nil {
			return err
		}
		printUser(u, false)
		return nil
	},
}

var registerCmd = &cobra.Command{
	Use:   "register <slug>",
	Short: "Step 'registered': submit the STACKIT sign-up form via Playwright",
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
		if err := r.Register(u, flagForce); err != nil {
			if errors.Is(err, browser.ErrAlreadyRegistered) {
				logf("hint: if this account was created by an earlier run, mark it with `stackit-acc user-set-step %s registered` and continue with verify-email", u.Slug)
			}
			return err
		}
		fmt.Printf("%s: step %s\n", u.Slug, u.Step)
		return nil
	},
}

var flagResend bool

var verifyEmailCmd = &cobra.Command{
	Use:   "verify-email <slug>",
	Short: "Step 'email_verified': wait for the STACKIT mails in MailHog and call the verification link",
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
		if err := r.VerifyEmail(ctx, u, flagForce, flagResend); err != nil {
			return err
		}
		fmt.Printf("%s: step %s\n", u.Slug, u.Step)
		return nil
	},
}

var setupOTPCmd = &cobra.Command{
	Use:   "setup-otp <slug>",
	Short: "Step 'otp_enabled': log in, enrol a TOTP authenticator and store the secret",
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
		if err := r.SetupOTP(u, flagForce); err != nil {
			return err
		}
		fmt.Printf("%s: step %s\n", u.Slug, u.Step)
		return nil
	},
}

func otpFor(u *store.User) (string, time.Duration, error) {
	if u.OTPSecret == "" {
		return "", 0, fmt.Errorf("user %s has no OTP secret yet (step %q)", u.Slug, u.Step)
	}
	return totp.Now(u.OTPSecret)
}

var otpCmd = &cobra.Command{
	Use:   "otp <slug>",
	Short: "Print the current TOTP code for a user",
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
		code, valid, err := otpFor(u)
		if err != nil {
			return err
		}
		if flagQuiet {
			fmt.Println(code)
			return nil
		}
		fmt.Printf("%s  (valid for %ds)\n", code, int(valid.Seconds()))
		return nil
	},
}

var flagCreate bool

var onboardCmd = &cobra.Command{
	Use:   "onboard <slug>...",
	Short: "Run all remaining account registration steps for the given users (--create adds unknown slugs first)",
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
			if errors.Is(err, store.ErrNotFound) && flagCreate {
				u, err = r.Create(slug, store.Participant{})
			}
			if err != nil {
				return err
			}
			if err := r.Onboard(ctx, u); err != nil {
				logf("onboard %s failed: %v", slug, err)
				failed = append(failed, slug)
				continue
			}
			fmt.Printf("%s: step %s\n", u.Slug, u.Step)
		}
		if len(failed) > 0 {
			return fmt.Errorf("onboarding failed for: %v", failed)
		}
		return nil
	},
}

func printUser(u *store.User, secrets bool) {
	cfg, _ := config.Load(flagConfig)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "slug:\t%s\n", u.Slug)
	fmt.Fprintf(w, "username:\t%s\n", u.Username)
	if secrets {
		fmt.Fprintf(w, "password:\t%s\n", u.Password)
	}
	fmt.Fprintf(w, "step:\t%s\n", u.Step)
	fmt.Fprintf(w, "participant:\t%s <%s>\n", u.Participant.Name, u.Participant.Email)
	if u.Project.ID != "" {
		fmt.Fprintf(w, "stackit project:\t%s\n", u.Project.ID)
		fmt.Fprintf(w, "  console:\t%s\n", cfg.StackitProjectURL(u.Project.ID))
	}
	if u.Project.ServiceAccount.Email != "" {
		fmt.Fprintf(w, "  service account:\t%s (key: %s)\n", u.Project.ServiceAccount.Email, u.Project.ServiceAccount.KeyFile)
	}
	if u.MeshstackWorkspace != "" {
		fmt.Fprintf(w, "meshstack workspace:\t%s\n", u.MeshstackWorkspace)
		fmt.Fprintf(w, "  url:\t%s\n", cfg.MeshstackWorkspaceURL(u.MeshstackWorkspace))
	}
	if secrets && u.OTPSecret != "" {
		fmt.Fprintf(w, "otp secret:\t%s\n", u.OTPSecret)
		if code, valid, err := otpFor(u); err == nil {
			fmt.Fprintf(w, "otp now:\t%s (valid %ds)\n", code, int(valid.Seconds()))
		}
	}
	if secrets && len(u.RecoveryCodes) > 0 {
		fmt.Fprintf(w, "recovery codes:\t%s\n", strings.Join(u.RecoveryCodes, " "))
	}
	for _, s := range store.Order {
		if t, ok := u.Timeline[s]; ok {
			fmt.Fprintf(w, "  %s:\t%s\n", s, t.Local().Format(time.RFC3339))
		}
	}
	if u.Notes != "" {
		fmt.Fprintf(w, "notes:\t%s\n", u.Notes)
	}
	w.Flush()
}

func init() {
	for _, c := range []*cobra.Command{userAddCmd, userAssignCmd} {
		c.Flags().StringVar(&flagName, "name", "", "participant name")
		c.Flags().StringVar(&flagEmail, "email", "", "participant e-mail address")
	}
	for _, c := range []*cobra.Command{registerCmd, verifyEmailCmd, setupOTPCmd} {
		c.Flags().BoolVar(&flagForce, "force", false, "run even if the user is not at the expected previous step")
	}
	verifyEmailCmd.Flags().BoolVar(&flagResend, "resend", false, "first log in and click 'Code erneut senden' to request a fresh verification mail")
	otpCmd.Flags().BoolVarP(&flagQuiet, "quiet", "q", false, "print only the code")
	onboardCmd.Flags().BoolVar(&flagCreate, "create", false, "create users that do not exist yet (step 'created') before onboarding")
	stackitAccCmd.AddCommand(userAddCmd, userListCmd, userShowCmd, userAssignCmd, userRemoveCmd, userSetProjectCmd, userSetStepCmd,
		registerCmd, verifyEmailCmd, setupOTPCmd, otpCmd, onboardCmd)
	rootCmd.AddCommand(stackitAccCmd)
}
