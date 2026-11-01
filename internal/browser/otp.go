package browser

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// Login signs in with username/password and waits for the MFA page (prompt = enrol, verify = code).
func (s *Session) Login(portalURL, username, password string) error {
	log := s.opts.Log
	p := s.Page
	log("open %s", portalURL)
	if _, err := p.Goto(portalURL); err != nil {
		return s.fail("login-goto", err)
	}
	if err := p.WaitForURL("https://accounts.stackit.cloud/ui/login/user*"); err != nil {
		return s.fail("login-page", fmt.Errorf("wait for IDP login page: %w", err))
	}
	user := s.textbox("Benutzername", false)
	if err := user.Fill(username); err != nil {
		return s.fail("login-username", err)
	}
	if err := user.Press("Enter"); err != nil {
		return s.fail("login-username-enter", err)
	}
	if err := s.textbox("Passwort", false).Fill(password); err != nil {
		return s.fail("login-password", err)
	}
	log("logging in as %s", username)
	if err := s.button("Einloggen").Click(); err != nil {
		return s.fail("login-submit", err)
	}
	if err := p.WaitForURL("https://accounts.stackit.cloud/ui/login/mfa/**"); err != nil {
		if msg := s.errorMessage(); msg != "" {
			return s.fail("login-error", fmt.Errorf("IDP reported: %s", msg))
		}
		return s.fail("login-mfa", fmt.Errorf("wait for MFA page: %w", err))
	}
	log("MFA page reached: %s", p.URL())
	return nil
}

// TOTPResult is the outcome of SetupTOTP.
type TOTPResult struct {
	Secret        string
	RecoveryCodes []string
	// AlreadyEnrolled is true when the login hit the MFA code prompt instead of the enrolment page.
	AlreadyEnrolled bool
	FinalURL        string
}

var recoveryCodeRe = regexp.MustCompile(`\b[A-Za-z0-9]{12}\b`)

// SetupTOTP walks through the authenticator-app enrolment on the MFA prompt page.
// codeFor is called with the secret shown by the IDP (so the caller can persist it) and must
// return the current TOTP code. existingSecret (optional) is used if the IDP only asks for a code.
func (s *Session) SetupTOTP(existingSecret string, codeFor func(secret string) (string, error)) (*TOTPResult, error) {
	log := s.opts.Log
	p := s.Page
	res := &TOTPResult{}

	if strings.Contains(p.URL(), "/mfa/verify") {
		if existingSecret == "" {
			return nil, s.fail("otp-verify-unexpected", fmt.Errorf("IDP asks for an OTP code but no secret is known"))
		}
		log("MFA already enrolled, verifying with the stored secret")
		res.AlreadyEnrolled = true
		res.Secret = existingSecret
		code, err := codeFor(existingSecret)
		if err != nil {
			return nil, err
		}
		if err := s.textbox("Code", false).Fill(code); err != nil {
			return nil, s.fail("otp-verify-code", err)
		}
		if err := s.button("Verifizieren").Click(); err != nil {
			return nil, s.fail("otp-verify-confirm", err)
		}
	} else {
		if err := p.Locator(".mfa-img").First().Click(); err != nil {
			return nil, s.fail("otp-choose-app", err)
		}
		if err := s.button("Einrichten").Click(); err != nil {
			return nil, s.fail("otp-einrichten", err)
		}
		if err := s.button("auch von Hand in").Click(); err != nil {
			return nil, s.fail("otp-manual", err)
		}
		heading := p.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Ihr Geheimschlüssel"})
		if err := s.Expect.Locator(heading).ToBeVisible(); err != nil {
			// occasionally the first click does not open the modal (seen under parallel load); try once more
			log("secret modal did not open, clicking again")
			if err := s.button("auch von Hand in").Click(); err != nil {
				return nil, s.fail("otp-manual", err)
			}
			if err := s.Expect.Locator(heading).ToBeVisible(); err != nil {
				return nil, s.fail("otp-secret-modal", err)
			}
		}
		secret, err := p.Locator("#mfa-secret").InputValue()
		if err != nil {
			return nil, s.fail("otp-secret-read", err)
		}
		secret = strings.TrimSpace(secret)
		if secret == "" {
			return nil, s.fail("otp-secret-empty", fmt.Errorf("#mfa-secret is empty"))
		}
		res.Secret = secret
		log("received MFA secret (%d chars)", len(secret))
		if err := s.button("Fertig").Click(); err != nil {
			return res, s.fail("otp-close-modal", err)
		}
		code, err := codeFor(secret)
		if err != nil {
			return res, err
		}
		if err := s.textbox("Code", false).Fill(code); err != nil {
			return res, s.fail("otp-code", err)
		}
		log("confirming initial OTP code")
		if err := s.button("Bestätigen").Click(); err != nil {
			return res, s.fail("otp-confirm", err)
		}
	}

	// Walk through the remaining IDP pages (recovery codes, ...) until we leave the login UI.
	deadline := time.Now().Add(2 * s.opts.Timeout)
	lastURL := ""
	for {
		_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
		url := p.URL()
		if msg := s.errorMessage(); msg != "" {
			return res, s.fail("otp-error", fmt.Errorf("IDP reported: %s", msg))
		}
		if !strings.Contains(url, "accounts.stackit.cloud/ui/login/") {
			res.FinalURL = url
			log("MFA done, page is now %s", url)
			s.Screenshot("otp-done")
			return res, nil
		}
		if url != lastURL {
			lastURL = url
			log("IDP page: %s", url)
			if strings.Contains(url, "/mfa/init/recovery_codes") {
				res.RecoveryCodes = scrapeRecoveryCodes(p)
				log("captured %d recovery codes", len(res.RecoveryCodes))
			}
			// Continue with the page's primary action ("Weiter", "Fertig", ...).
			if next := p.Locator(".lgn-actions button.lgn-primary, button.lgn-primary, button[type=submit]").First(); isVisible(next) {
				name, _ := next.InnerText()
				log("clicking %q", strings.TrimSpace(name))
				if err := next.Click(); err != nil {
					return res, s.fail("otp-next", err)
				}
				continue
			}
		}
		if time.Now().After(deadline) {
			return res, s.fail("otp-stuck", fmt.Errorf("still inside the IDP login UI after MFA"))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func isVisible(l playwright.Locator) bool {
	v, err := l.IsVisible()
	return err == nil && v
}

// scrapeRecoveryCodes extracts the recovery codes shown on the IDP page. The download button carries
// them verbatim in data-codes; the `.codes-box` spans and a regex over the page text are fallbacks.
func scrapeRecoveryCodes(p playwright.Page) []string {
	if v, err := p.Locator("#lgn-download-button").GetAttribute("data-codes"); err == nil && strings.TrimSpace(v) != "" {
		return strings.Fields(v)
	}
	if spans, err := p.Locator(".codes-box span").AllInnerTexts(); err == nil && len(spans) > 0 {
		var codes []string
		for _, c := range spans {
			if c = strings.TrimSpace(c); c != "" {
				codes = append(codes, c)
			}
		}
		return codes
	}
	text, err := p.Locator("body").InnerText()
	if err != nil {
		return nil
	}
	if i := strings.Index(text, "Kopieren"); i > 0 {
		text = text[:i]
	}
	var codes []string
	seen := map[string]bool{}
	for _, tok := range recoveryCodeRe.FindAllString(text, -1) {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		codes = append(codes, tok)
	}
	return codes
}
