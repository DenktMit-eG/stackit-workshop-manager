package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/denktmit/stackit-ws-manager/internal/stackitapi"
	"github.com/denktmit/stackit-ws-manager/internal/store"
	"github.com/denktmit/stackit-ws-manager/internal/terraform"
)

func (r *Runner) tfProject(u *store.User) *terraform.Project {
	return &terraform.Project{
		Binary: r.Cfg.TF.Binary,
		Dir:    filepath.Join(r.Cfg.TF.WorkDir, u.Slug),
		Log:    r.logf,
	}
}

func expandSlug(tmpl, slug string) string { return strings.ReplaceAll(tmpl, "{slug}", slug) }

// TerraformPrepare copies the template, installs the service account key and writes the tfvars.
func (r *Runner) TerraformPrepare(u *store.User) (*terraform.Project, error) {
	if u.Project.ID == "" {
		return nil, fmt.Errorf("user %s has no STACKIT project", u.Slug)
	}
	keyPath := r.KeyPath(u)
	if keyPath == "" {
		return nil, fmt.Errorf("user %s has no service account key", u.Slug)
	}
	p := r.tfProject(u)
	created, err := p.CopyTemplate(r.Cfg.TF.TemplateDir)
	if err != nil {
		return nil, fmt.Errorf("copy template: %w", err)
	}
	if created {
		r.logf("copied template %s -> %s", r.Cfg.TF.TemplateDir, p.Dir)
	} else {
		r.logf("refreshed template files in existing project directory %s (state kept)", p.Dir)
	}
	keyFile, pemFile, err := p.InstallKey(keyPath)
	if err != nil {
		return nil, err
	}
	v := terraform.Tfvars{
		ProjectID:  u.Project.ID,
		NamePrefix: expandSlug(r.Cfg.TF.Tfvars.NamePrefix, u.Slug),
		NameSuffix: expandSlug(r.Cfg.TF.Tfvars.NameSuffix, u.Slug),
		Domain:     expandSlug(r.Cfg.TF.Tfvars.Domain, u.Slug),
		KeyFile:    keyFile,
		PemFile:    pemFile,
	}
	if err := p.WriteTfvars(v); err != nil {
		return nil, err
	}
	r.logf("tfvars: project %s, domain %s, names %s-*-%s", v.ProjectID, v.Domain, v.NamePrefix, v.NameSuffix)
	u.Terraform.Dir = u.Slug
	_ = r.Store.Save(u)
	return p, nil
}

// TerraformDeploy (step "terraform_deployed") prepares, inits, applies, writes the outputs and zips the project.
func (r *Runner) TerraformDeploy(ctx context.Context, u *store.User, force bool) error {
	if err := r.require(u, store.StepServiceAccountCreated, force); err != nil {
		return err
	}
	p, err := r.TerraformPrepare(u)
	if err != nil {
		return err
	}
	if err := r.EnableObjectStorage(ctx, u); err != nil {
		return err
	}
	r.logf("terraform init")
	if err := p.Init(ctx); err != nil {
		return err
	}
	r.logf("terraform apply (this takes a while)")
	start := time.Now()
	if err := p.Apply(ctx); err != nil {
		// transient API errors are common (e.g. two buckets racing to enable object storage for the
		// project: 500 / 409 "Two concurrent calls"); terraform apply is idempotent, so try once more
		r.logf("apply failed (%v), retrying once in 30s", err)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(30 * time.Second):
		}
		if err := p.Apply(ctx); err != nil {
			return err
		}
	}
	r.logf("terraform apply finished after %s", time.Since(start).Round(time.Second))
	if _, err := p.WriteOutputEnv(ctx); err != nil {
		r.logf("warning: %v", err)
	}
	if err := r.TerraformZip(u); err != nil {
		return err
	}
	u.Terraform.AppliedAt = time.Now().UTC().Truncate(time.Second)
	u.Advance(store.StepTerraformDeployed)
	return r.Store.Save(u)
}

// EnableObjectStorage enables the Object Storage service in the user's project via the API
// (the terraform provider's implicit enablement races when several buckets are created at once).
func (r *Runner) EnableObjectStorage(ctx context.Context, u *store.User) error {
	keyPath := r.KeyPath(u)
	if keyPath == "" || u.Project.ID == "" {
		return fmt.Errorf("user %s has no project / service account key", u.Slug)
	}
	p := r.tfProject(u)
	_, pemFile, err := p.InstallKey(keyPath)
	if err != nil {
		return err
	}
	return stackitapi.EnableObjectStorage(ctx, keyPath, filepath.Join(p.Dir, pemFile), r.Cfg.TF.Region, u.Project.ID, r.logf)
}

// TerraformZip packs the user's project directory into <work_dir>/<slug>.zip.
func (r *Runner) TerraformZip(u *store.User) error {
	p := r.tfProject(u)
	if _, err := os.Stat(p.Dir); err != nil {
		return fmt.Errorf("no terraform project for %s: %w", u.Slug, err)
	}
	zipName := u.Slug + ".zip"
	if err := p.Zip(filepath.Join(r.Cfg.TF.WorkDir, zipName)); err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	u.Terraform.Dir = u.Slug
	u.Terraform.Zip = zipName
	u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	r.logf("zipped to %s", filepath.Join(r.Cfg.TF.WorkDir, zipName))
	return r.Store.Save(u)
}

// TerraformDestroy destroys the deployed resources (project directory and zip are kept).
func (r *Runner) TerraformDestroy(ctx context.Context, u *store.User) error {
	p := r.tfProject(u)
	if _, err := os.Stat(filepath.Join(p.Dir, "main.tf")); err != nil {
		return fmt.Errorf("no terraform project for %s", u.Slug)
	}
	if err := p.Init(ctx); err != nil {
		return err
	}
	if err := p.Destroy(ctx); err != nil {
		return err
	}
	if u.Step == store.StepTerraformDeployed {
		u.Step = store.StepServiceAccountCreated
	}
	u.Terraform.AppliedAt = time.Time{}
	u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	return r.Store.Save(u)
}

// TerraformZipPath returns the absolute path of the user's zip, or "".
func (r *Runner) TerraformZipPath(u *store.User) string {
	if u.Terraform.Zip == "" {
		return ""
	}
	return filepath.Join(r.Cfg.TF.WorkDir, filepath.Base(u.Terraform.Zip))
}

// TerraformOutputs returns the content of output/project.env, or "".
func (r *Runner) TerraformOutputs(u *store.User) string {
	data, err := os.ReadFile(filepath.Join(r.Cfg.TF.WorkDir, u.Slug, "output", "project.env"))
	if err != nil {
		return ""
	}
	return string(data)
}

// Jumphost describes the deployed jumphost of a user as far as the terraform outputs and state know it.
type Jumphost struct {
	FQDN     string
	IP       string
	User     string // ssh user
	Password string // ssh password; also the password of the web shell
}

// WebshellUser is the basic-auth user of the GoTTY web shell behind Caddy.
const WebshellUser = "shell"

// SSHUser is the login user created by cloud-init.
const SSHUser = "ubuntu"

// WebshellURL returns https://shell:<password>@<fqdn>/ (credentials embedded for one-click access).
func (j Jumphost) WebshellURL() string {
	u := url.URL{Scheme: "https", Host: j.FQDN, Path: "/", User: url.UserPassword(WebshellUser, j.Password)}
	return u.String()
}

// PlainURL returns https://<fqdn>/ without credentials.
func (j Jumphost) PlainURL() string { return "https://" + j.FQDN + "/" }

// SSHCommand returns the ssh command line.
func (j Jumphost) SSHCommand() string { return "ssh " + j.User + "@" + j.FQDN }

// TerraformJumphost reads the jumphost details from output/project.env (JUMPHOST1_JUMPHOST_*), with
// the public IP taken from the terraform state when the outputs do not contain it. Returns nil when
// nothing is deployed yet.
func (r *Runner) TerraformJumphost(u *store.User) *Jumphost {
	env := parseEnv(r.TerraformOutputs(u))
	j := &Jumphost{FQDN: env["JUMPHOST1_JUMPHOST_FQDN"], IP: env["JUMPHOST1_JUMPHOST_IP"], Password: env["JUMPHOST1_JUMPHOST_PASSWORD"], User: SSHUser}
	if j.FQDN == "" {
		return nil
	}
	j.FQDN = strings.TrimSuffix(j.FQDN, ".")
	if j.IP == "" {
		j.IP = publicIPFromState(filepath.Join(r.Cfg.TF.WorkDir, u.Slug, "terraform.tfstate"), "jumphost")
	}
	return j
}

// parseEnv reads KEY='value' lines as written by WriteOutputEnv.
func parseEnv(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], `'\''`, "'")
		}
		out[k] = v
	}
	return out
}

// publicIPFromState returns the ip attribute of the first stackit_public_ip resource whose module
// address contains moduleHint.
func publicIPFromState(path, moduleHint string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state struct {
		Resources []struct {
			Module    string `json:"module"`
			Type      string `json:"type"`
			Instances []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	for _, res := range state.Resources {
		if res.Type != "stackit_public_ip" || !strings.Contains(res.Module, moduleHint) || len(res.Instances) == 0 {
			continue
		}
		if ip, ok := res.Instances[0].Attributes["ip"].(string); ok {
			return ip
		}
	}
	return ""
}
