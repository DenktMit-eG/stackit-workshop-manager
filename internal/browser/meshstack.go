package browser

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// LoginIDP completes the STACKIT IDP login (username, password, OTP) on the current page.
// The page must already be on https://accounts.stackit.cloud/ui/login/user*.
func (s *Session) LoginIDP(username, password string, codeFor func() (string, error)) error {
	log := s.opts.Log
	p := s.Page
	if err := p.WaitForURL("https://accounts.stackit.cloud/ui/login/user*"); err != nil {
		return s.fail("idp-login-page", fmt.Errorf("wait for IDP login page: %w", err))
	}
	user := s.textbox("Benutzername", false)
	if err := user.Fill(username); err != nil {
		return s.fail("idp-username", err)
	}
	if err := user.Press("Enter"); err != nil {
		return s.fail("idp-username-enter", err)
	}
	pw := s.textbox("Passwort", false)
	if err := pw.Fill(password); err != nil {
		return s.fail("idp-password", err)
	}
	log("logging in as %s", username)
	if err := pw.Press("Enter"); err != nil {
		return s.fail("idp-password-enter", err)
	}
	if err := p.WaitForURL("https://accounts.stackit.cloud/ui/login/mfa/verify*"); err != nil {
		if msg := s.errorMessage(); msg != "" {
			return s.fail("idp-login-error", fmt.Errorf("IDP reported: %s", msg))
		}
		return s.fail("idp-mfa", fmt.Errorf("wait for MFA code page: %w", err))
	}
	// The IDP refuses a TOTP code that was already used (e.g. by another login a moment ago);
	// in that case wait for the next 30 s window and try once more.
	for attempt := 1; ; attempt++ {
		code, err := codeFor()
		if err != nil {
			return err
		}
		codeBox := s.textbox("Code", false)
		if err := codeBox.Fill(code); err != nil {
			return s.fail("idp-code", err)
		}
		if err := codeBox.Press("Enter"); err != nil {
			return s.fail("idp-code-enter", err)
		}
		_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateLoad})
		msg := s.errorMessage()
		if msg == "" {
			return nil
		}
		if attempt < 2 && strings.Contains(msg, "Code") {
			wait := 31*time.Second - time.Duration(time.Now().Unix()%30)*time.Second
			log("IDP rejected the OTP code (%s), retrying with the next code in %s", msg, wait)
			time.Sleep(wait)
			continue
		}
		return s.fail("idp-code-error", fmt.Errorf("IDP reported: %s", msg))
	}
}

// ErrMeshSetupBusy is returned when meshStack answers the sign-up with "Something went wrong /
// We're still setting up part of your workspace" (seen under many parallel sign-ups). Continuing
// there yields a workspace without a project, so the caller should retry later instead.
var ErrMeshSetupBusy = errors.New("meshStack workspace setup is busy (\"Something went wrong\")")

// MeshProjectResult is the outcome of CreateMeshProject.
type MeshProjectResult struct {
	WorkspaceURL string
	WorkspaceID  string
	ConsoleURL   string
	ProjectID    string
}

var projectIDRe = regexp.MustCompile(`portal\.stackit\.cloud/projects/([0-9a-fA-F-]{36})`)

// CreateMeshProject signs up for a meshStack trial workspace (organization = orgName) and returns the
// UUID of the STACKIT project that meshStack provisions.
func (s *Session) CreateMeshProject(setupURL, orgName, username, password string, codeFor func() (string, error), setupTimeout time.Duration) (*MeshProjectResult, error) {
	log := s.opts.Log
	p := s.Page
	log("open %s", setupURL)
	if _, err := p.Goto(setupURL); err != nil {
		return nil, s.fail("mesh-goto", err)
	}
	if err := s.LoginIDP(username, password, codeFor); err != nil {
		return nil, err
	}
	res := &MeshProjectResult{}

	// After the IDP login either the sign-up form ("One more thing", still on setup.try.meshstack.io,
	// fresh user) or an existing workspace overview on try.meshstack.io shows up.
	org := s.textbox("Organization name", false)
	overview := regexp.MustCompile(`https://try\.meshstack\.io/#/w/[^/]+/overview`)
	deadline := time.Now().Add(s.opts.Timeout)
	for {
		s.dismissMeshDialogs()
		if isVisible(org) {
			log("filling organization name %q", orgName)
			if err := org.Fill(orgName); err != nil {
				return nil, s.fail("mesh-orgname", err)
			}
			if err := s.button("Continue to meshStack").Click(); err != nil {
				return nil, s.fail("mesh-continue", err)
			}
			heading := p.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Setting up your workspace"})
			wrong := p.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Something went wrong"})
			if err := s.Expect.Locator(heading.Or(wrong)).ToBeVisible(); err != nil {
				return nil, s.fail("mesh-setup-heading", err)
			}
			if isVisible(wrong) {
				return nil, s.fail("mesh-setup-busy", ErrMeshSetupBusy)
			}
			log("workspace setup started, waiting up to %s for terraform apply ...", setupTimeout)
			s.Screenshot("mesh-setup")
			// the recorded flow clicks the "Run Terraform Apply" item of the progress list; harmless
			if li := p.GetByRole(*playwright.AriaRoleListitem).Filter(playwright.LocatorFilterOptions{HasText: "Run Terraform Apply"}); isVisible(li) {
				_ = li.Click()
			}
			break
		}
		if overview.MatchString(p.URL()) {
			log("workspace already exists")
			break
		}
		if time.Now().After(deadline) {
			return nil, s.fail("mesh-unknown-page", fmt.Errorf("neither sign-up form nor workspace overview found"))
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err := p.WaitForURL(overview, playwright.PageWaitForURLOptions{Timeout: playwright.Float(float64(setupTimeout.Milliseconds()))}); err != nil {
		return nil, s.fail("mesh-setup-timeout", fmt.Errorf("workspace overview did not appear: %w", err))
	}
	res.WorkspaceURL = p.URL()
	if m := regexp.MustCompile(`#/w/([^/]+)/`).FindStringSubmatch(res.WorkspaceURL); m != nil {
		res.WorkspaceID = m[1]
	}
	log("workspace ready: %s (id %s)", res.WorkspaceURL, res.WorkspaceID)
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	s.dismissMeshDialogs()
	if err := s.Expect.Locator(p.GetByText("1 projects and their")).ToBeVisible(); err != nil {
		s.Screenshot("mesh-overview")
		log("warning: project count text not found (%v), continuing", err)
	}
	link := p.GetByRole(*playwright.AriaRoleLink, playwright.PageGetByRoleOptions{Name: "Open Console"})
	if err := link.First().WaitFor(); err != nil {
		return nil, s.fail("mesh-console-link", fmt.Errorf("'Open Console' link not found: %w", err))
	}
	href, err := link.First().GetAttribute("href")
	if err != nil || href == "" {
		return nil, s.fail("mesh-console-href", fmt.Errorf("'Open Console' link has no href"))
	}
	res.ConsoleURL = href
	m := projectIDRe.FindStringSubmatch(href)
	if m == nil {
		return nil, s.fail("mesh-project-id", fmt.Errorf("no project UUID in console link %q", href))
	}
	res.ProjectID = strings.ToLower(m[1])
	log("STACKIT project %s", res.ProjectID)
	s.Screenshot("mesh-done")
	return res, nil
}

// ServiceAccountResult is the outcome of CreateServiceAccount.
type ServiceAccountResult struct {
	Email   string
	KeyPath string
	Role    string
}

// CreateServiceAccount logs into the STACKIT portal at projectURL, creates a service account named
// <name>-... in the project, grants it `role` on the project and downloads a key as JSON to keyPath.
func (s *Session) CreateServiceAccount(projectURL, name, role, username, password string, codeFor func() (string, error), keyPath string) (*ServiceAccountResult, error) {
	log := s.opts.Log
	p := s.Page
	log("open %s", projectURL)
	if _, err := p.Goto(projectURL); err != nil {
		return nil, s.fail("sa-goto", err)
	}
	if err := s.LoginIDP(username, password, codeFor); err != nil {
		return nil, err
	}
	if err := p.WaitForURL("https://portal.stackit.cloud/**"); err != nil {
		return nil, s.fail("sa-portal", fmt.Errorf("wait for portal: %w", err))
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	// first login into the portal: terms of use dialog
	if consent := s.button("ZUSTIMMEN"); isVisible(consent) {
		log("accepting terms (ZUSTIMMEN)")
		if err := consent.Click(); err != nil {
			return nil, s.fail("sa-consent", err)
		}
	}
	if err := s.link("Rollen").Click(); err != nil {
		return nil, s.fail("sa-menu-rollen", err)
	}
	if err := s.link("Service Accounts").Click(); err != nil {
		return nil, s.fail("sa-menu-sa", err)
	}
	// wait for the list page (create button) and its table to settle before looking for an existing account
	if err := s.button("Service Account erstellen").WaitFor(); err != nil {
		return nil, s.fail("sa-list", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	emailRe := regexp.MustCompile(regexp.QuoteMeta(name) + `-[^\s@]+@sa\.stackit\.cloud`)
	row := p.GetByRole(*playwright.AriaRoleRow, playwright.PageGetByRoleOptions{Name: emailRe}).First()
	if err := row.WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(5000)}); err == nil {
		log("reusing existing service account")
	} else {
		if err := s.button("Service Account erstellen").Click(); err != nil {
			return nil, s.fail("sa-create-button", err)
		}
		if err := s.textbox("E-Mail-Adresse des Service", false).Fill(name); err != nil {
			return nil, s.fail("sa-name", err)
		}
		if err := s.button("Erstellen").Click(); err != nil {
			return nil, s.fail("sa-create", err)
		}
		if err := row.WaitFor(); err != nil {
			return nil, s.fail("sa-row", fmt.Errorf("service account row not found: %w", err))
		}
	}
	rowText, _ := row.InnerText()
	email := emailRe.FindString(rowText)
	log("service account %s", email)
	res := &ServiceAccountResult{Email: email}
	if err := row.Click(); err != nil {
		return res, s.fail("sa-row-click", err)
	}
	if err := s.link("Service Account Keys").Click(); err != nil {
		return res, s.fail("sa-keys-tab", err)
	}
	// Right after project creation the key permissions may not have propagated yet (403 page), and on
	// slow hosts the page needs a moment; re-open the keys URL until the create button is enabled.
	// (A plain reload may drop the portal back to the dashboard, so navigate to the remembered URL.)
	keyButton := p.Locator(`[data-test-id="create-service-account-key-button"]`).GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{Name: "Service Account Schlüssel"})
	keysURL := ""
	for attempt := 1; ; attempt++ {
		_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
		if keysURL == "" && strings.Contains(p.URL(), "/keys") {
			keysURL = p.URL()
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if enabled, _ := keyButton.IsEnabled(); enabled && isVisible(keyButton) {
				break
			}
			time.Sleep(time.Second)
		}
		if enabled, _ := keyButton.IsEnabled(); enabled && isVisible(keyButton) {
			break
		}
		denied := isVisible(p.GetByText("Zugriff verweigert"))
		if attempt >= 12 {
			return res, s.fail("sa-keys-access", fmt.Errorf("service account keys page not usable (access denied: %v)", denied))
		}
		log("keys page not ready yet (access denied: %v), re-opening in 10s (attempt %d)", denied, attempt)
		time.Sleep(10 * time.Second)
		if keysURL == "" {
			return res, s.fail("sa-keys-url", fmt.Errorf("lost the service account keys page (url %s)", p.URL()))
		}
		if _, err := p.Goto(keysURL); err != nil {
			return res, s.fail("sa-keys-reopen", err)
		}
	}
	if err := keyButton.Click(); err != nil {
		return res, s.fail("sa-key-button", err)
	}
	if err := p.Locator(`[data-test-id="toggle-option-false"]`).Click(); err != nil {
		return res, s.fail("sa-key-toggle", err)
	}
	if err := p.Locator(`[data-test-id="create-service-acc-key"]`).Click(); err != nil {
		return res, s.fail("sa-key-create", err)
	}
	if err := s.button("Herunterladen").Click(); err != nil {
		return res, s.fail("sa-download-menu", err)
	}
	dl, err := p.ExpectDownload(func() error {
		return p.GetByRole(*playwright.AriaRoleMenuitem, playwright.PageGetByRoleOptions{Name: "Herunterladen JSON"}).Click()
	})
	if err != nil {
		return res, s.fail("sa-download", err)
	}
	if err := dl.SaveAs(keyPath); err != nil {
		return res, s.fail("sa-save", fmt.Errorf("save key: %w", err))
	}
	res.KeyPath = keyPath
	log("key saved to %s", keyPath)
	s.Screenshot("sa-done")
	if role != "" {
		if _, err := s.GrantProjectRole(email, role); err != nil {
			return res, err
		}
		res.Role = role
	}
	return res, nil
}

// dismissMeshDialogs closes the cookie banner and the "Welcome to your meshStack trial" experience
// chooser (we pick the Application Team view, which is what workshop participants use).
func (s *Session) dismissMeshDialogs() {
	if b := s.button("Decline"); isVisible(b) {
		s.opts.Log("declining cookies")
		_ = b.Click()
	}
	if b := s.button("Start as Application Team"); isVisible(b) {
		s.opts.Log("closing trial welcome dialog (Application Team)")
		_ = b.Click()
		time.Sleep(time.Second)
	}
}

func (s *Session) link(name string) playwright.Locator {
	return s.Page.GetByRole(*playwright.AriaRoleLink, playwright.PageGetByRoleOptions{Name: name})
}

// LoginProject opens projectURL, logs in via the IDP and accepts the terms dialog if shown.
func (s *Session) LoginProject(projectURL, username, password string, codeFor func() (string, error)) error {
	p := s.Page
	if _, err := p.Goto(projectURL); err != nil {
		return s.fail("project-goto", err)
	}
	if err := s.LoginIDP(username, password, codeFor); err != nil {
		return err
	}
	if err := p.WaitForURL("https://portal.stackit.cloud/**"); err != nil {
		return s.fail("project-portal", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	if consent := s.button("ZUSTIMMEN"); isVisible(consent) {
		_ = consent.Click()
	}
	return nil
}

// InspectAccess logs into the project, opens the IAM "Access" page and returns its text (debugging aid).
func (s *Session) InspectAccess(projectURL, username, password string, codeFor func() (string, error)) (string, error) {
	p := s.Page
	if _, err := p.Goto(projectURL); err != nil {
		return "", s.fail("access-goto", err)
	}
	if err := s.LoginIDP(username, password, codeFor); err != nil {
		return "", err
	}
	if err := p.WaitForURL("https://portal.stackit.cloud/**"); err != nil {
		return "", s.fail("access-portal", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	if consent := s.button("ZUSTIMMEN"); isVisible(consent) {
		_ = consent.Click()
	}
	if err := s.link("Access").Click(); err != nil {
		return "", s.fail("access-menu", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	time.Sleep(3 * time.Second)
	s.Screenshot("access")
	txt, err := p.Locator("main, body").First().InnerText()
	return txt, err
}

// InspectGrantDialog opens the "Zugriff gewähren" dialog on the Access page and returns its HTML (debugging aid).
func (s *Session) InspectGrantDialog(projectURL, username, password string, codeFor func() (string, error)) (string, error) {
	if _, err := s.InspectAccess(projectURL, username, password, codeFor); err != nil {
		return "", err
	}
	p := s.Page
	if err := s.button("Zugriff gewähren").Click(); err != nil {
		return "", s.fail("grant-button", err)
	}
	time.Sleep(2 * time.Second)
	s.Screenshot("grant-dialog")
	html, err := p.Locator("[role=dialog], .modal, .cdk-overlay-container").First().InnerHTML()
	if err != nil {
		html, err = p.Locator("body").InnerHTML()
	}
	return html, err
}

// GrantProjectRole gives member (user or service account e-mail) the role on the project via the
// IAM "Access" page ("Zugriff gewähren"). The page must be logged in and inside the project already.
// It returns false if the member already had the role.
func (s *Session) GrantProjectRole(member, role string) (granted bool, err error) {
	log := s.opts.Log
	p := s.Page
	if err := s.link("Access").Click(); err != nil {
		return false, s.fail("grant-menu", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	if err := s.button("Zugriff gewähren").WaitFor(); err != nil {
		return false, s.fail("grant-page", err)
	}
	row := p.GetByRole(*playwright.AriaRoleRow).Filter(playwright.LocatorFilterOptions{HasText: member})
	if err := row.First().WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(5000)}); err == nil {
		txt, _ := row.First().InnerText()
		if strings.Contains(txt, role) {
			log("%s already has role %s", member, role)
			return false, nil
		}
	}
	if err := s.button("Zugriff gewähren").Click(); err != nil {
		return false, s.fail("grant-button", err)
	}
	search := p.Locator(`[data-test-id="shared-add-user-input"]`)
	if err := search.Fill(member); err != nil {
		return false, s.fail("grant-search", err)
	}
	// the search result row shows the e-mail with a "Hinzufügen" button
	if err := p.GetByText(member, playwright.PageGetByTextOptions{Exact: playwright.Bool(true)}).First().WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(15000)}); err != nil {
		return false, s.fail("grant-suggestion", fmt.Errorf("no search result for %s: %w", member, err))
	}
	if err := p.Locator(`[data-test-id="shared-add-user-input-results"]`).GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{Name: "Hinzufügen"}).First().Click(); err != nil {
		return false, s.fail("grant-suggestion-add", err)
	}
	// The role field is a read-only combobox; its panel renders in a cdk overlay with
	// <div role="option" id="owner"> Owner </div> entries that re-render while the panel settles,
	// so opening + clicking is retried a few times.
	roleBox := p.GetByRole(*playwright.AriaRoleCombobox, playwright.PageGetByRoleOptions{Name: "Rolle auswählen"})
	option := p.Locator(`[data-test-id="form-select-values-listbox"] [role=option][id="` + role + `"]`).First()
	var clickErr error
	for attempt := 1; attempt <= 4; attempt++ {
		if !isVisible(option) {
			if err := roleBox.Click(); err != nil {
				clickErr = err
				continue
			}
			if err := option.WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(5000)}); err != nil {
				clickErr = err
				continue
			}
		}
		time.Sleep(500 * time.Millisecond) // let the panel settle
		if clickErr = option.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)}); clickErr == nil {
			break
		}
		log("role option click attempt %d failed: %v", attempt, clickErr)
	}
	if clickErr != nil {
		s.DumpHTML("grant-role-options")
		return false, s.fail("grant-role-option", fmt.Errorf("could not select role %q: %w", role, clickErr))
	}
	confirm := p.Locator(`[data-test-id="confirm"]`)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cls, _ := confirm.GetAttribute("class"); !strings.Contains(cls, "disabled") {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	s.Screenshot("grant-filled")
	if err := confirm.Click(); err != nil {
		return false, s.fail("grant-confirm", err)
	}
	if err := p.GetByText("Zugriff erfolgreich gewährt.").First().WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(20000)}); err != nil {
		log("warning: success toast not seen (%v), checking the table instead", err)
	}
	_ = p.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	// verify
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if txt, err := row.First().InnerText(); err == nil && strings.Contains(txt, role) {
			log("granted role %s to %s", role, member)
			s.Screenshot("grant-done")
			return true, nil
		}
		time.Sleep(time.Second)
	}
	return false, s.fail("grant-verify", fmt.Errorf("%s does not show up with role %s after saving", member, role))
}
