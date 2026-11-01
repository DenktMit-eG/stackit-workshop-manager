// Package config loads the general (non-secret) tool configuration from a YAML file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the general configuration of stackit-ws-manager.
type Config struct {
	// CatchallDomain is the domain whose mails end up in the mail sink (e.g. "uniphi.de").
	CatchallDomain string `yaml:"catchall_domain"`
	// MailhogURL is the base URL of the MailHog instance (no trailing slash needed).
	MailhogURL string `yaml:"mailhog_url"`
	// UsersFile is the path to the user directory file (relative to the config file if not absolute).
	UsersFile string `yaml:"users_file"`
	// PortalURL is the STACKIT portal entry point; it redirects to the IDP login.
	PortalURL string `yaml:"portal_url"`

	Mail    MailConfig      `yaml:"mail"`
	Browser BrowserConfig   `yaml:"browser"`
	Web     WebConfig       `yaml:"web"`
	Mesh    MeshConfig      `yaml:"meshstack"`
	URLs    URLTemplates    `yaml:"urls"`
	TF      TerraformConfig `yaml:"terraform"`
}

// TerraformConfig configures the per-user terraform deployment.
type TerraformConfig struct {
	Binary      string `yaml:"binary"`       // terraform or tofu
	Region      string `yaml:"region"`       // STACKIT region for API calls (object storage enablement)
	TemplateDir string `yaml:"template_dir"` // copied for every user
	WorkDir     string `yaml:"work_dir"`     // <work_dir>/<slug>/ is the user's terraform project, <work_dir>/<slug>.zip the download
	// Tfvars are written to teilnehmer.auto.tfvars; "{slug}" is replaced by the user slug.
	Tfvars TfvarsTemplate `yaml:"tfvars"`
}

// TfvarsTemplate holds the participant-specific terraform variables.
type TfvarsTemplate struct {
	NamePrefix string `yaml:"name_prefix"`
	NameSuffix string `yaml:"name_suffix"`
	Domain     string `yaml:"domain"`
}

// URLTemplates reconstruct portal links from the stored ids ("{id}" is replaced).
type URLTemplates struct {
	StackitProject     string `yaml:"stackit_project"`
	MeshstackWorkspace string `yaml:"meshstack_workspace"`
}

func expand(tmpl, id string) string {
	if id == "" || tmpl == "" {
		return ""
	}
	return strings.ReplaceAll(tmpl, "{id}", id)
}

// StackitProjectURL returns the console URL of a project.
func (c Config) StackitProjectURL(projectID string) string {
	return expand(c.URLs.StackitProject, projectID)
}

// MeshstackWorkspaceURL returns the overview URL of a meshStack workspace.
func (c Config) MeshstackWorkspaceURL(workspaceID string) string {
	return expand(c.URLs.MeshstackWorkspace, workspaceID)
}

// MeshConfig configures the meshStack trial onboarding.
type MeshConfig struct {
	// SetupURL starts the meshStack trial sign-up (redirects to the STACKIT IDP).
	SetupURL string `yaml:"setup_url"`
	// SetupTimeout bounds the "Setting up your workspace" phase (terraform apply, several minutes).
	SetupTimeout time.Duration `yaml:"setup_timeout"`
	// OrgNamePrefix is put in front of the slug for the meshStack organization name when the slug is
	// too short or starts with a digit (e.g. slug "1" -> "project1").
	OrgNamePrefix string `yaml:"org_name_prefix"`
	// ServiceAccountName is the local part prefix of the service account ("ws" -> ws-<random>@sa.stackit.cloud).
	ServiceAccountName string `yaml:"service_account_name"`
	// ServiceAccountRole is granted to the service account on the project (needed for terraform).
	ServiceAccountRole string `yaml:"service_account_role"`
	// KeyDir stores the downloaded service account key JSON files (relative to the config file).
	KeyDir string `yaml:"key_dir"`
}

// WebConfig configures the built-in web UI (`serve`).
type WebConfig struct {
	Listen string `yaml:"listen"`
	// Password protects the UI when AuthEnabled is true. Stored in plain text on purpose (workshop tooling).
	Password    string `yaml:"password"`
	AuthEnabled bool   `yaml:"auth_enabled"`
	// MailDir is the local archive of all mails received for the users (relative to the config file).
	MailDir          string        `yaml:"mail_dir"`
	MailSyncInterval time.Duration `yaml:"mail_sync_interval"`
}

// MailConfig tunes how long we wait for mails to arrive.
type MailConfig struct {
	WaitTimeout  time.Duration `yaml:"wait_timeout"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

// BrowserConfig tunes the Playwright browser session.
type BrowserConfig struct {
	Headless      bool          `yaml:"headless"`
	SlowMo        time.Duration `yaml:"slow_mo"`
	Timeout       time.Duration `yaml:"timeout"`
	Locale        string        `yaml:"locale"`
	ScreenshotDir string        `yaml:"screenshot_dir"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		CatchallDomain: "uniphi.de",
		MailhogURL:     "https://mailhog.mentala.de",
		UsersFile:      "users.yaml",
		PortalURL:      "https://portal.stackit.cloud/",
		Mail: MailConfig{
			WaitTimeout:  10 * time.Minute,
			PollInterval: 5 * time.Second,
		},
		Browser: BrowserConfig{
			Headless:      true,
			SlowMo:        0,
			Timeout:       30 * time.Second,
			Locale:        "de-DE",
			ScreenshotDir: "screenshots",
		},
		Mesh: MeshConfig{
			SetupURL:           "https://setup.try.meshstack.io/?ref=stackit-university",
			SetupTimeout:       20 * time.Minute,
			OrgNamePrefix:      "project",
			ServiceAccountName: "ws",
			ServiceAccountRole: "owner",
			KeyDir:             "keys",
		},
		TF: TerraformConfig{
			Binary:      "terraform",
			Region:      "eu01",
			TemplateDir: "terraform-template",
			WorkDir:     "terraform",
			Tfvars: TfvarsTemplate{
				NamePrefix: "tn",
				NameSuffix: "g1",
				Domain:     "customer-lz{slug}.runs.onstackit.cloud",
			},
		},
		URLs: URLTemplates{
			StackitProject:     "https://portal.stackit.cloud/projects/{id}",
			MeshstackWorkspace: "https://try.meshstack.io/#/w/{id}/overview",
		},
		Web: WebConfig{
			Listen:           ":8080",
			Password:         "workshop",
			AuthEnabled:      false,
			MailDir:          "mail",
			MailSyncInterval: 30 * time.Second,
		},
	}
}

// Load reads the config file at path. Missing keys fall back to Default().
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w (run `init` to create one)", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.CatchallDomain == "" {
		return cfg, fmt.Errorf("config %s: catchall_domain must be set", path)
	}
	if cfg.MailhogURL == "" {
		return cfg, fmt.Errorf("config %s: mailhog_url must be set", path)
	}
	if !filepath.IsAbs(cfg.UsersFile) {
		cfg.UsersFile = filepath.Join(filepath.Dir(path), cfg.UsersFile)
	}
	if !filepath.IsAbs(cfg.Browser.ScreenshotDir) {
		cfg.Browser.ScreenshotDir = filepath.Join(filepath.Dir(path), cfg.Browser.ScreenshotDir)
	}
	if !filepath.IsAbs(cfg.Mesh.KeyDir) {
		cfg.Mesh.KeyDir = filepath.Join(filepath.Dir(path), cfg.Mesh.KeyDir)
	}
	if !filepath.IsAbs(cfg.TF.TemplateDir) {
		cfg.TF.TemplateDir = filepath.Join(filepath.Dir(path), cfg.TF.TemplateDir)
	}
	if !filepath.IsAbs(cfg.TF.WorkDir) {
		cfg.TF.WorkDir = filepath.Join(filepath.Dir(path), cfg.TF.WorkDir)
	}
	if !filepath.IsAbs(cfg.Web.MailDir) {
		cfg.Web.MailDir = filepath.Join(filepath.Dir(path), cfg.Web.MailDir)
	}
	return cfg, nil
}

// Write serialises cfg to path (used by `init`).
func Write(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := "# stackit-ws-manager configuration\n"
	return os.WriteFile(path, append([]byte(header), data...), 0o644)
}
