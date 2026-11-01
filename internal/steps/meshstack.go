package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode"

	"github.com/denktmit/stackit-ws-manager/internal/browser"
	"github.com/denktmit/stackit-ws-manager/internal/store"
	"github.com/denktmit/stackit-ws-manager/internal/totp"
)

func (r *Runner) codeFor(u *store.User) func() (string, error) {
	return func() (string, error) {
		code, _, err := totp.Now(u.OTPSecret)
		return code, err
	}
}

// MeshOrgName derives the meshStack organization (workspace) name from the slug. meshStack rejects
// very short names, and names such as "1" are confusing anyway, so slugs that are shorter than
// three characters or do not start with a letter get the configured prefix (default "project").
func (r *Runner) MeshOrgName(slug string) string {
	prefix := r.Cfg.Mesh.OrgNamePrefix
	if prefix == "" {
		prefix = "project"
	}
	if len(slug) < 3 || !unicode.IsLetter(rune(slug[0])) {
		return prefix + slug
	}
	return slug
}

// MeshProject (step "project_assigned") creates the meshStack trial workspace and records the STACKIT project UUID.
func (r *Runner) MeshProject(u *store.User, force bool) error {
	if err := r.require(u, store.StepOTPEnabled, force); err != nil {
		return err
	}
	if u.OTPSecret == "" {
		return fmt.Errorf("user %s has no OTP secret", u.Slug)
	}
	return retry(r, 4, 60*time.Second, browser.ErrMeshSetupBusy, func() error {
		s, err := browser.New(r.browserOptions(u))
		if err != nil {
			return err
		}
		defer s.Close()
		res, err := s.CreateMeshProject(r.Cfg.Mesh.SetupURL, r.MeshOrgName(u.Slug), u.Username, u.Password, r.codeFor(u), r.Cfg.Mesh.SetupTimeout)
		if err != nil {
			return fmt.Errorf("meshstack project %s: %w", u.Slug, err)
		}
		u.Project.ID = res.ProjectID
		u.MeshstackWorkspace = res.WorkspaceID
		u.Advance(store.StepProjectAssigned)
		return r.Store.Save(u)
	})
}

// ServiceAccount (step "service_account_created") creates a service account + key in the user's project.
func (r *Runner) ServiceAccount(u *store.User, force bool) error {
	if err := r.require(u, store.StepProjectAssigned, force); err != nil {
		return err
	}
	if u.Project.ID == "" {
		return fmt.Errorf("user %s has no project id", u.Slug)
	}
	if err := os.MkdirAll(r.Cfg.Mesh.KeyDir, 0o700); err != nil {
		return err
	}
	file := fmt.Sprintf("%s-sa-key-%s.json", u.Slug, time.Now().Format("20060102-150405"))
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return err
	}
	defer s.Close()
	res, err := s.CreateServiceAccount(r.Cfg.StackitProjectURL(u.Project.ID), r.Cfg.Mesh.ServiceAccountName, r.Cfg.Mesh.ServiceAccountRole, u.Username, u.Password, r.codeFor(u), filepath.Join(r.Cfg.Mesh.KeyDir, file))
	if res != nil && res.Email != "" && u.Project.ServiceAccount.Email != res.Email {
		u.Project.ServiceAccount.Email = res.Email
		u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		_ = r.Store.Save(u)
	}
	if err != nil {
		return fmt.Errorf("service account %s: %w", u.Slug, err)
	}
	_ = os.Chmod(res.KeyPath, 0o600)
	u.Project.ServiceAccount = store.ServiceAccount{Email: res.Email, KeyFile: file, Role: res.Role}
	u.Advance(store.StepServiceAccountCreated)
	return r.Store.Save(u)
}

// MeshWatch is the debugging helper behind `meshstack watch`.
func (r *Runner) MeshWatch(u *store.User, duration time.Duration) error {
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Watch(r.Cfg.Mesh.SetupURL, u.Username, u.Password, r.codeFor(u), duration, 3*time.Second)
}

// GrantRole gives the user's service account the configured project role (idempotent).
func (r *Runner) GrantRole(u *store.User) error {
	if u.Project.ID == "" || u.Project.ServiceAccount.Email == "" {
		return fmt.Errorf("user %s has no project / service account yet", u.Slug)
	}
	role := r.Cfg.Mesh.ServiceAccountRole
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.LoginProject(r.Cfg.StackitProjectURL(u.Project.ID), u.Username, u.Password, r.codeFor(u)); err != nil {
		return err
	}
	if _, err := s.GrantProjectRole(u.Project.ServiceAccount.Email, role); err != nil {
		return fmt.Errorf("grant role %s: %w", u.Slug, err)
	}
	u.Project.ServiceAccount.Role = role
	u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	return r.Store.Save(u)
}

// MeshInspectAccess is the debugging helper behind `meshstack inspect-access`.
func (r *Runner) MeshInspectAccess(u *store.User) (string, error) {
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return "", err
	}
	defer s.Close()
	return s.InspectAccess(r.Cfg.StackitProjectURL(u.Project.ID), u.Username, u.Password, r.codeFor(u))
}

// MeshInspectGrant is the debugging helper behind `meshstack inspect-grant`.
func (r *Runner) MeshInspectGrant(u *store.User) (string, error) {
	s, err := browser.New(r.browserOptions(u))
	if err != nil {
		return "", err
	}
	defer s.Close()
	return s.InspectGrantDialog(r.Cfg.StackitProjectURL(u.Project.ID), u.Username, u.Password, r.codeFor(u))
}

// KeyPath returns the absolute path of the user's service account key, or "".
func (r *Runner) KeyPath(u *store.User) string {
	if u.Project.ServiceAccount.KeyFile == "" {
		return ""
	}
	return filepath.Join(r.Cfg.Mesh.KeyDir, filepath.Base(u.Project.ServiceAccount.KeyFile))
}

// MeshOnboard runs the meshStack project + service account steps.
func (r *Runner) MeshOnboard(u *store.User) error {
	for {
		switch u.Step {
		case store.StepOTPEnabled:
			r.logf("== %s: meshstack project", u.Slug)
			if err := r.MeshProject(u, false); err != nil {
				return err
			}
		case store.StepProjectAssigned:
			r.logf("== %s: service account", u.Slug)
			if err := r.ServiceAccount(u, false); err != nil {
				return err
			}
		case store.StepServiceAccountCreated:
			r.logf("== %s: meshStack onboarding complete", u.Slug)
			return nil
		default:
			return fmt.Errorf("user %s must reach step %q first (is at %q)", u.Slug, store.StepOTPEnabled, u.Step)
		}
	}
}
