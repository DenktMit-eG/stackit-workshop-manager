// Package steps implements the account-creation milestones on top of the store,
// the mail sink and the browser flows. Every function persists its progress.
package steps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/denktmit/stackit-ws-manager/internal/browser"
	"github.com/denktmit/stackit-ws-manager/internal/config"
	"github.com/denktmit/stackit-ws-manager/internal/mailhog"
	"github.com/denktmit/stackit-ws-manager/internal/password"
	"github.com/denktmit/stackit-ws-manager/internal/store"
	"github.com/denktmit/stackit-ws-manager/internal/totp"
)

const (
	stackitMailSender = "info@accounts.stackit.cloud"
	welcomeSubject    = "Willkommen bei STACKIT"
	verifySubject     = "Verifizieren Sie Ihre E-Mail-Adresse"
	verifiedOKMarker  = "Email verified successfully"
	passwordLength    = 16
)

var verifyLinkRe = regexp.MustCompile(`https://accounts\.stackit\.cloud/ui/login/mail/verification\?_r=[A-Za-z0-9_\-]+`)
var verifyErrorMarkers = []string{"lgn-error-message", "Invalid one-time code provided"}

// Runner carries the shared dependencies.
type Runner struct {
	Cfg   config.Config
	Store *store.Store
	Log   func(format string, args ...any)
	// Headless overrides the config when non-nil (CLI --headful).
	Headless *bool
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}

func (r *Runner) browserOptions(u *store.User) browser.Options {
	headless := r.Cfg.Browser.Headless
	if r.Headless != nil {
		headless = *r.Headless
	}
	return browser.Options{
		Headless:      headless,
		SlowMo:        r.Cfg.Browser.SlowMo,
		Timeout:       r.Cfg.Browser.Timeout,
		Locale:        r.Cfg.Browser.Locale,
		ScreenshotDir: r.Cfg.Browser.ScreenshotDir,
		Prefix:        u.Slug,
		Log:           r.logf,
	}
}

// Create (step "created") adds a user with a freshly generated password.
func (r *Runner) Create(slug string, participant store.Participant) (*store.User, error) {
	if err := store.ValidSlug(slug); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	u := &store.User{
		Slug:        slug,
		Username:    slug + "@" + r.Cfg.CatchallDomain,
		Password:    password.Generate(passwordLength),
		Participant: participant,
		CreatedAt:   now,
	}
	u.Advance(store.StepCreated)
	if err := r.Store.Add(u); err != nil {
		return nil, err
	}
	if err := r.Store.Save(u); err != nil {
		return nil, err
	}
	r.logf("created %s (%s)", u.Slug, u.Username)
	return u, nil
}

func (r *Runner) require(u *store.User, want store.Step, force bool) error {
	if force {
		return nil
	}
	if u.Step != want {
		return fmt.Errorf("user %s is at step %q, expected %q (use --force to override)", u.Slug, u.Step, want)
	}
	return nil
}

// Register (step "registered") submits the STACKIT sign-up form. An HTTP 429 (rate limit) is retried
// with growing pauses.
func (r *Runner) Register(u *store.User, force bool) error {
	if err := r.require(u, store.StepCreated, force); err != nil {
		return err
	}
	return retry(r, 4, 90*time.Second, browser.ErrRateLimited, func() error {
		s, err := browser.New(r.browserOptions(u))
		if err != nil {
			return err
		}
		defer s.Close()
		if _, err := s.Register(r.Cfg.PortalURL, u.Username, u.Password); err != nil {
			return fmt.Errorf("register %s: %w", u.Slug, err)
		}
		u.Advance(store.StepRegistered)
		return r.Store.Save(u)
	})
}

// retry runs fn up to attempts times while it fails with retryable; the pause grows linearly.
func retry(r *Runner, attempts int, pause time.Duration, retryable error, fn func() error) error {
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil || !errors.Is(err, retryable) || attempt >= attempts {
			return err
		}
		wait := time.Duration(attempt) * pause
		r.logf("%v - retrying in %s (attempt %d/%d)", retryable, wait, attempt+1, attempts)
		time.Sleep(wait)
	}
}

// VerifyEmail (step "email_verified") waits for the welcome + verification mails and calls the link.
func (r *Runner) VerifyEmail(ctx context.Context, u *store.User, force, resend bool) error {
	if err := r.require(u, store.StepRegistered, force); err != nil {
		return err
	}
	if resend {
		if err := r.ResendVerification(u); err != nil {
			return err
		}
		force = false
		u.Timeline[store.StepRegistered] = time.Now().UTC().Truncate(time.Second)
	}
	// Only consider mails that arrived after the sign-up (with slack for clock skew); with --force
	// (or unknown sign-up time) accept any mail in the mailbox.
	var since time.Time
	if t, ok := u.Timeline[store.StepRegistered]; ok && !force {
		since = t.Add(-2 * time.Minute)
	}
	mh := mailhog.New(r.Cfg.MailhogURL)
	ctx, cancel := context.WithTimeout(ctx, r.Cfg.Mail.WaitTimeout)
	defer cancel()

	// Milestone 1 (welcome mail) is informational: it is not sent reliably (e.g. after a resend),
	// so only the verification mail is awaited. Both are reported when seen.
	r.logf("waiting for %q mail to %s ...", verifySubject, u.Username)
	welcomeSeen := false
	isWelcome := mailhog.SubjectFrom(stackitMailSender, welcomeSubject)
	isVerify := mailhog.SubjectFrom(stackitMailSender, verifySubject)
	m, err := mh.WaitFor(ctx, u.Username, since, r.Cfg.Mail.PollInterval, func(m *mailhog.Message) bool {
		if !welcomeSeen && isWelcome(m) {
			welcomeSeen = true
			r.logf("milestone 1: welcome mail received (%s)", m.Created.Local().Format(time.RFC3339))
		}
		return isVerify(m)
	})
	if err != nil {
		return err
	}
	r.logf("milestone 2: verification mail received (%s)", m.Created.Local().Format(time.RFC3339))
	link := verifyLinkRe.FindString(m.Text())
	if link == "" {
		return fmt.Errorf("no verification link found in mail %s", m.ID)
	}
	r.logf("calling verification link")
	if err := callVerificationLink(ctx, link); err != nil {
		return err
	}
	r.logf("e-mail address verified")
	u.Advance(store.StepEmailVerified)
	return r.Store.Save(u)
}

// ResendVerification asks the IDP to send the verification mail again (login with the unverified account).
func (r *Runner) ResendVerification(u *store.User) error {
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.ResendVerification(r.Cfg.PortalURL, u.Username, u.Password); err != nil {
		return fmt.Errorf("resend verification %s: %w", u.Slug, err)
	}
	return nil
}

func callVerificationLink(ctx context.Context, link string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "stackit-ws-manager/1.0")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET verification link: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	text := string(body)
	for _, marker := range verifyErrorMarkers {
		if strings.Contains(text, marker) {
			return fmt.Errorf("verification failed (HTTP %d): response contains %q", resp.StatusCode, marker)
		}
	}
	if !strings.Contains(text, verifiedOKMarker) {
		return fmt.Errorf("verification response (HTTP %d) does not contain %q; starts with: %.300s", resp.StatusCode, verifiedOKMarker, strings.TrimSpace(text))
	}
	return nil
}

// SetupOTP (step "otp_enabled") logs in and enrols a TOTP authenticator; the secret is stored.
func (r *Runner) SetupOTP(u *store.User, force bool) error {
	if err := r.require(u, store.StepEmailVerified, force); err != nil {
		return err
	}
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.Login(r.Cfg.PortalURL, u.Username, u.Password); err != nil {
		return fmt.Errorf("login %s: %w", u.Slug, err)
	}
	res, err := s.SetupTOTP(u.OTPSecret, func(secret string) (string, error) {
		// persist immediately: if confirming fails we still know the secret the IDP showed
		u.OTPSecret = totp.Normalize(secret)
		u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		if err := r.Store.Save(u); err != nil {
			return "", err
		}
		code, _, err := totp.Now(secret)
		return code, err
	})
	if res != nil && len(res.RecoveryCodes) > 0 {
		u.RecoveryCodes = res.RecoveryCodes
		_ = r.Store.Save(u)
	}
	if err != nil {
		if res != nil && res.Secret != "" {
			return fmt.Errorf("setup otp %s (secret stored, enrolment not confirmed): %w", u.Slug, err)
		}
		return fmt.Errorf("setup otp %s: %w", u.Slug, err)
	}
	u.OTPSecret = totp.Normalize(res.Secret)
	if res.AlreadyEnrolled {
		r.logf("MFA was already enrolled for %s; stored secret verified by logging in", u.Slug)
	}
	u.Advance(store.StepOTPEnabled)
	return r.Store.Save(u)
}

// OTP returns the current TOTP code for the user.
func (r *Runner) OTP(u *store.User) (code string, validFor time.Duration, err error) {
	if u.OTPSecret == "" {
		return "", 0, fmt.Errorf("user %s has no OTP secret yet (step %q)", u.Slug, u.Step)
	}
	return totp.Now(u.OTPSecret)
}

// Onboard runs all remaining steps up to otp_enabled.
func (r *Runner) Onboard(ctx context.Context, u *store.User) error {
	for {
		switch u.Step {
		case store.StepCreated:
			r.logf("== %s: register", u.Slug)
			if err := r.Register(u, false); err != nil {
				return err
			}
		case store.StepRegistered:
			r.logf("== %s: verify-email", u.Slug)
			if err := r.VerifyEmail(ctx, u, false, false); err != nil {
				return err
			}
		case store.StepEmailVerified:
			r.logf("== %s: setup-otp", u.Slug)
			if err := r.SetupOTP(u, false); err != nil {
				return err
			}
		default:
			r.logf("== %s: nothing left to do (step %s)", u.Slug, u.Step)
			return nil
		}
	}
}
