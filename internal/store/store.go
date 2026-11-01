// Package store persists the workshop user directory in a YAML file.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// Step names the account-creation milestone a user has reached.
type Step string

const (
	// StepCreated: slug chosen and password generated, nothing done at STACKIT yet.
	StepCreated Step = "created"
	// StepRegistered: the sign-up form at accounts.stackit.cloud was submitted.
	StepRegistered Step = "registered"
	// StepEmailVerified: the verification link from the mail was called successfully.
	StepEmailVerified Step = "email_verified"
	// StepOTPEnabled: TOTP MFA is activated and the secret is stored.
	StepOTPEnabled Step = "otp_enabled"
	// StepProjectAssigned: a STACKIT project UUID has been assigned (meshStack trial workspace created).
	StepProjectAssigned Step = "project_assigned"
	// StepServiceAccountCreated: a service account + key exist in the STACKIT project.
	StepServiceAccountCreated Step = "service_account_created"
	// StepTerraformDeployed: the terraform landing zone was applied in the project and zipped.
	StepTerraformDeployed Step = "terraform_deployed"
)

// Order lists the steps in the sequence they must be achieved.
var Order = []Step{StepCreated, StepRegistered, StepEmailVerified, StepOTPEnabled, StepProjectAssigned, StepServiceAccountCreated, StepTerraformDeployed}

// Index returns the position of the step within Order, or -1.
func (s Step) Index() int {
	for i, o := range Order {
		if o == s {
			return i
		}
	}
	return -1
}

// Reached reports whether s is at least as advanced as other.
func (s Step) Reached(other Step) bool { return s.Index() >= other.Index() }

// Participant is the real person a workshop account is handed to.
type Participant struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// TerraformState records the user's terraform deployment (paths relative to the terraform work dir).
type TerraformState struct {
	Dir       string    `yaml:"dir,omitempty"`
	Zip       string    `yaml:"zip,omitempty"`
	AppliedAt time.Time `yaml:"applied_at,omitempty"`
}

// StackitProject is the STACKIT project assigned to a user.
type StackitProject struct {
	ID             string         `yaml:"id,omitempty"` // project UUID
	ServiceAccount ServiceAccount `yaml:"service_account,omitempty"`
}

// ServiceAccount describes the STACKIT service account of a user's project.
type ServiceAccount struct {
	Email string `yaml:"email,omitempty"`
	// Role is the project role granted to the service account.
	Role string `yaml:"role,omitempty"`
	// KeyFile is the downloaded key JSON (path relative to the key directory).
	KeyFile string `yaml:"key_file,omitempty"`
}

// User is one workshop account.
type User struct {
	Slug      string `yaml:"slug"`
	Username  string `yaml:"username"` // the STACKIT login = e-mail address
	Password  string `yaml:"password"`
	OTPSecret string `yaml:"otp_secret"`
	// RecoveryCodes are the one-time MFA recovery codes shown by the IDP after enrolment.
	RecoveryCodes []string `yaml:"recovery_codes,omitempty"`
	// Project is the STACKIT project provisioned by meshStack (incl. the service account inside it).
	Project StackitProject `yaml:"stackit_project,omitempty"`
	// Terraform describes the deployed landing zone.
	Terraform TerraformState `yaml:"terraform,omitempty"`
	// MeshstackWorkspace is the meshStack workspace id (the <id> in https://try.meshstack.io/#/w/<id>/overview).
	MeshstackWorkspace string `yaml:"meshstack_workspace,omitempty"`

	// legacy fields (files written before 2026-10-17 afternoon); migrated by Load
	LegacyProjectID      string             `yaml:"project_id,omitempty"`
	LegacyServiceAccount ServiceAccount     `yaml:"service_account,omitempty"`
	Step                 Step               `yaml:"step"`
	Participant          Participant        `yaml:"participant"`
	CreatedAt            time.Time          `yaml:"created_at"`
	UpdatedAt            time.Time          `yaml:"updated_at"`
	Timeline             map[Step]time.Time `yaml:"timeline,omitempty"`
	Notes                string             `yaml:"notes,omitempty"`
}

// Advance records that u has reached step now.
func (u *User) Advance(step Step) {
	if u.Timeline == nil {
		u.Timeline = map[Step]time.Time{}
	}
	now := time.Now().UTC().Truncate(time.Second)
	u.Timeline[step] = now
	u.UpdatedAt = now
	if step.Index() > u.Step.Index() {
		u.Step = step
	}
}

// Directory is the on-disk document.
type Directory struct {
	Users []*User `yaml:"users"`
}

// Store wraps a Directory with its file path.
type Store struct {
	Path    string
	Dir     Directory
	removed map[string]bool
}

// ErrNotFound is returned when a slug is unknown.
var ErrNotFound = errors.New("user not found")

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ValidSlug checks that slug can be used as mailbox local part.
func ValidSlug(slug string) error {
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("invalid slug %q: use lowercase letters, digits, '.', '_' or '-'", slug)
	}
	return nil
}

// Load reads the directory from path; a missing file yields an empty store.
func Load(path string) (*Store, error) {
	s := &Store{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read users file %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &s.Dir); err != nil {
		return nil, fmt.Errorf("parse users file %s: %w", path, err)
	}
	for _, u := range s.Dir.Users {
		u.migrate()
	}
	return s, nil
}

var workspaceIDRe = regexp.MustCompile(`#/w/([^/]+)/`)

// migrate converts records written by older versions.
func (u *User) migrate() {
	if u.LegacyProjectID != "" {
		if u.Project.ID == "" {
			u.Project.ID = u.LegacyProjectID
		}
		u.LegacyProjectID = ""
	}
	if u.LegacyServiceAccount.Email != "" || u.LegacyServiceAccount.KeyFile != "" {
		if u.Project.ServiceAccount.Email == "" {
			u.Project.ServiceAccount = u.LegacyServiceAccount
		}
		u.LegacyServiceAccount = ServiceAccount{}
	}
	if m := workspaceIDRe.FindStringSubmatch(u.MeshstackWorkspace); m != nil {
		u.MeshstackWorkspace = m[1]
	}
	if strings.HasPrefix(u.Notes, "meshStack console ") || strings.HasPrefix(u.Notes, "mfa done, portal at ") || strings.HasPrefix(u.Notes, "register ended at ") {
		u.Notes = ""
	}
}

// Save persists the directory atomically with restrictive permissions (it holds secrets).
//
// Several processes (CLI runs, later the web UI) may work on the file concurrently, so Save takes a
// file lock, re-reads the current on-disk state and merges this process' changes into it: users
// given as `changed` (or all in-memory users, if none are given) are written, users removed via
// Remove are dropped, everything else keeps its on-disk version.
func (s *Store) Save(changed ...*User) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	unlock, err := lock(s.Path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	disk, err := Load(s.Path)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		changed = s.Dir.Users
	}
	merged := map[string]*User{}
	for _, u := range disk.Dir.Users {
		merged[u.Slug] = u
	}
	for slug := range s.removed {
		delete(merged, slug)
	}
	for _, u := range changed {
		merged[u.Slug] = u
	}
	s.Dir.Users = s.Dir.Users[:0]
	for _, u := range merged {
		s.Dir.Users = append(s.Dir.Users, u)
	}
	sort.SliceStable(s.Dir.Users, func(i, j int) bool { return s.Dir.Users[i].Slug < s.Dir.Users[j].Slug })
	s.removed = nil

	data, err := yaml.Marshal(&s.Dir)
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Get returns the user with the given slug.
func (s *Store) Get(slug string) (*User, error) {
	for _, u := range s.Dir.Users {
		if u.Slug == slug {
			return u, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, slug)
}

// Add inserts a new user; the slug must be unique.
func (s *Store) Add(u *User) error {
	if _, err := s.Get(u.Slug); err == nil {
		return fmt.Errorf("user %s already exists", u.Slug)
	}
	s.Dir.Users = append(s.Dir.Users, u)
	return nil
}

// Remove deletes the user with the given slug.
func (s *Store) Remove(slug string) error {
	for i, u := range s.Dir.Users {
		if u.Slug == slug {
			s.Dir.Users = append(s.Dir.Users[:i], s.Dir.Users[i+1:]...)
			if s.removed == nil {
				s.removed = map[string]bool{}
			}
			s.removed[slug] = true
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, slug)
}
