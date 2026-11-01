package browser

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// ErrRateLimited is returned when the IDP answers the sign-up with HTTP 429 (too many sign-ups from one IP).
var ErrRateLimited = errors.New("sign-up rate limited by the IDP (HTTP 429)")

// RegisterResult describes where the sign-up flow ended.
type RegisterResult struct {
	FinalURL string
}

// Register fills the "Erstellen Sie ein Konto" form of the STACKIT IDP (Zitadel login UI).
//
// Playwright-recorded flow:
//
//	goto portalURL -> wait for accounts.stackit.cloud/ui/login/user?*
//	click "Erstellen Sie ein Konto"
//	fill E-Mail / Passwort / Passwort-Bestätigung
//	click "Weiter"  (POST /ui/login/register -> "Überprüfen Sie Ihren Email-Posteingang" page)
func (s *Session) Register(portalURL, username, password string) (*RegisterResult, error) {
	log := s.opts.Log
	p := s.Page

	log("open %s", portalURL)
	if _, err := p.Goto(portalURL); err != nil {
		return nil, s.fail("register-goto", err)
	}
	if err := p.WaitForURL("https://accounts.stackit.cloud/ui/login/user?*"); err != nil {
		return nil, s.fail("register-login-page", fmt.Errorf("wait for IDP login page: %w", err))
	}
	log("login page reached, opening sign-up form")
	// The button POSTs the login form with page=register; wait for the new document to be fully loaded,
	// otherwise the fills race with the page scripts.
	if err := s.button("Erstellen Sie ein Konto").Click(); err != nil {
		return nil, s.fail("register-open-form", err)
	}
	if err := p.Locator("#password-confirmation").WaitFor(); err != nil {
		return nil, s.fail("register-form", fmt.Errorf("sign-up form did not appear: %w", err))
	}
	if err := p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateLoad}); err != nil {
		return nil, s.fail("register-form-load", err)
	}
	if err := s.textbox("E-Mail", false).Fill(username); err != nil {
		return nil, s.fail("register-email", err)
	}
	if err := s.textbox("Passwort", true).Fill(password); err != nil {
		return nil, s.fail("register-password", err)
	}
	if err := s.textbox("Passwort-Bestätigung", false).Fill(password); err != nil {
		return nil, s.fail("register-password-confirm", err)
	}

	// Leave the password field so its policy dropdown fades out before the submit click.
	_, _ = p.Evaluate(`() => document.activeElement && document.activeElement.blur()`)
	time.Sleep(400 * time.Millisecond)

	// Submit: the click must result in a POST to /ui/login/register. Occasionally the click is swallowed
	// (no request leaves the browser); in that case it is safe to click again.
	log("submitting sign-up form for %s", username)
	var resp playwright.Response
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		resp, err = p.ExpectResponse("**/ui/login/register", func() error {
			return s.button("Weiter").Click()
		}, playwright.PageExpectResponseOptions{Timeout: playwright.Float(10000)})
		if err == nil {
			break
		}
		if !errors.Is(err, playwright.ErrTimeout) && !strings.Contains(err.Error(), "Timeout") {
			return nil, s.fail("register-submit", err)
		}
		if msg := s.errorMessage(); msg != "" {
			return nil, s.registerError(msg)
		}
		log("submit click produced no request (attempt %d), retrying", attempt)
	}
	if resp == nil {
		return nil, s.fail("register-no-post", fmt.Errorf("sign-up form was not submitted"))
	}
	if resp.Status() == 429 {
		return nil, s.fail("register-http", ErrRateLimited)
	}
	if resp.Status() >= 400 {
		return nil, s.fail("register-http", fmt.Errorf("sign-up POST returned HTTP %d", resp.Status()))
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateLoad})

	// Success: the IDP moves on to the mail verification page. Failure: form re-rendered with toast/field error.
	deadline := time.Now().Add(s.opts.Timeout)
	for {
		if msg := s.errorMessage(); msg != "" {
			return nil, s.registerError(msg)
		}
		if !isVisible(p.Locator("#password-confirmation")) {
			break
		}
		if time.Now().After(deadline) {
			return nil, s.fail("register-stuck", fmt.Errorf("sign-up form still visible after submit"))
		}
		time.Sleep(500 * time.Millisecond)
	}
	res := &RegisterResult{FinalURL: p.URL()}
	log("sign-up submitted, page is now %s", res.FinalURL)
	s.Screenshot("register-done")
	return res, nil
}

func (s *Session) registerError(msg string) error {
	if strings.Contains(msg, "bereits registriert") {
		return s.fail("register-exists", fmt.Errorf("%w: %s", ErrAlreadyRegistered, msg))
	}
	return s.fail("register-error", fmt.Errorf("IDP reported: %s", msg))
}
