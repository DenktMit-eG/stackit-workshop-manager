package browser

import (
	"fmt"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// ResendVerification logs in with an unverified account; the IDP then shows the
// "Überprüfen Sie Ihren Email-Posteingang" page where "Code erneut senden" triggers a new mail.
func (s *Session) ResendVerification(portalURL, username, password string) error {
	log := s.opts.Log
	p := s.Page
	log("open %s", portalURL)
	if _, err := p.Goto(portalURL); err != nil {
		return s.fail("resend-goto", err)
	}
	if err := p.WaitForURL("https://accounts.stackit.cloud/ui/login/user*"); err != nil {
		return s.fail("resend-login-page", fmt.Errorf("wait for IDP login page: %w", err))
	}
	user := s.textbox("Benutzername", false)
	if err := user.Fill(username); err != nil {
		return s.fail("resend-username", err)
	}
	if err := user.Press("Enter"); err != nil {
		return s.fail("resend-username-enter", err)
	}
	if err := s.textbox("Passwort", false).Fill(password); err != nil {
		return s.fail("resend-password", err)
	}
	log("logging in as %s to reach the mail verification page", username)
	if err := s.button("Einloggen").Click(); err != nil {
		return s.fail("resend-submit", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateLoad})
	if msg := s.errorMessage(); msg != "" {
		return s.fail("resend-login-error", fmt.Errorf("IDP reported: %s", msg))
	}
	link := p.GetByText("Code erneut senden").First()
	if err := link.WaitFor(); err != nil {
		if strings.Contains(p.URL(), "/mfa/") {
			return s.fail("resend-already-verified", fmt.Errorf("account is already verified (IDP went to %s)", p.URL()))
		}
		return s.fail("resend-no-link", fmt.Errorf("mail verification page not shown: %w", err))
	}
	if err := link.Click(); err != nil {
		return s.fail("resend-click", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	if msg := s.errorMessage(); msg != "" {
		return s.fail("resend-error", fmt.Errorf("IDP reported: %s", msg))
	}
	log("verification mail re-requested (page: %s)", p.URL())
	s.Screenshot("resend-done")
	return nil
}
