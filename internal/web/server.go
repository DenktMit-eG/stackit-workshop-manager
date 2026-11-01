// Package web serves the password-manager style UI on top of users.yaml.
// Plain HTML + Go templates; reactivity via Datastar (SSE patches pushed from the server).
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/denktmit/stackit-ws-manager/internal/config"
	"github.com/denktmit/stackit-ws-manager/internal/events"
	"github.com/denktmit/stackit-ws-manager/internal/jobs"
	"github.com/denktmit/stackit-ws-manager/internal/mailhog"
	"github.com/denktmit/stackit-ws-manager/internal/mailstore"
	"github.com/denktmit/stackit-ws-manager/internal/steps"
	"github.com/denktmit/stackit-ws-manager/internal/store"
	"github.com/denktmit/stackit-ws-manager/internal/totp"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server is the web UI.
type Server struct {
	cfg      config.Config
	headless *bool
	bus      *events.Bus
	jobs     *jobs.Manager
	mails    *mailstore.Store
	mh       *mailhog.Client
	tmpl     *template.Template
	token    string // session cookie value when auth is enabled
	logger   *log.Logger
}

// New creates the server. headless overrides the browser config when non-nil.
func New(cfg config.Config, headless *bool) (*Server, error) {
	bus := events.New()
	s := &Server{
		cfg:      cfg,
		headless: headless,
		bus:      bus,
		jobs:     jobs.New(bus),
		mails:    &mailstore.Store{Dir: cfg.Web.MailDir},
		mh:       mailhog.New(cfg.MailhogURL),
		logger:   log.New(os.Stderr, "web ", log.LstdFlags),
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	sum := sha256.Sum256(append([]byte(cfg.Web.Password+"|"), nonce...))
	s.token = hex.EncodeToString(sum[:])

	tmpl, err := template.New("").Funcs(s.funcs()).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl
	return s, nil
}

// Run serves until ctx is done. It also runs the file watcher and the mail syncer.
func (s *Server) Run(ctx context.Context) error {
	go s.watchUsersFile(ctx)
	go s.mailSyncLoop(ctx)
	srv := &http.Server{Addr: s.cfg.Web.Listen, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	s.logger.Printf("listening on %s (auth enabled: %v)", s.cfg.Web.Listen, s.cfg.Web.AuthEnabled)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler builds the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("POST /users", s.handleAddUser)
	mux.HandleFunc("GET /users/{slug}", s.handleUser)
	mux.HandleFunc("GET /users/{slug}/events", s.handleUserEvents)
	mux.HandleFunc("GET /otps", s.handleOTPs)
	mux.HandleFunc("GET /users/{slug}/sa-key.json", s.handleSAKey)
	mux.HandleFunc("GET /users/{slug}/terraform.zip", s.handleTerraformZip)
	mux.HandleFunc("POST /users/{slug}/run/{action}", s.handleRun)
	mux.HandleFunc("POST /users/{slug}/participant", s.handleParticipant)
	mux.HandleFunc("POST /users/{slug}/project", s.handleProject)
	mux.HandleFunc("POST /users/{slug}/mail/sync", s.handleMailSync)
	mux.HandleFunc("GET /users/{slug}/mail/{file}", s.handleMail)
	mux.HandleFunc("GET /users/{slug}/mail/{file}/html", s.handleMailHTML)
	mux.HandleFunc("GET /screenshots/{file}", s.handleScreenshot)
	return s.auth(mux)
}

// ---- auth ----------------------------------------------------------------------------------------

const cookieName = "swm_session"

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Web.AuthEnabled || strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(cookieName); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Datastar-Request") == "true" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "login", map[string]any{"Error": r.URL.Query().Get("error")})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(s.cfg.Web.Password)) != 1 {
		http.Redirect(w, r, "/login?error=wrong+password", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- background: file watcher + mail sync ----------------------------------------------------------

func (s *Server) watchUsersFile(ctx context.Context) {
	stamp := func() string {
		fi, err := os.Stat(s.cfg.UsersFile)
		if err != nil {
			return ""
		}
		return fi.ModTime().String() + "/" + fmt.Sprint(fi.Size())
	}
	last := stamp()
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cur := stamp(); cur != last {
				last = cur
				s.bus.Publish(events.Event{Kind: "users"})
			}
		}
	}
}

func (s *Server) mailSyncLoop(ctx context.Context) {
	interval := s.cfg.Web.MailSyncInterval
	if interval <= 0 {
		return
	}
	// archives written by older versions may hold the same mail many times (moving "Created" time)
	if removed, err := s.mails.Dedupe(); err != nil {
		s.logger.Printf("mail dedupe: %v", err)
	} else {
		for slug, n := range removed {
			s.logger.Printf("mail dedupe: removed %d duplicate(s) for %s", n, slug)
		}
	}
	for {
		s.syncMail(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (s *Server) syncMail(ctx context.Context) {
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		s.logger.Printf("mail sync: %v", err)
		return
	}
	added, err := s.mails.Sync(ctx, s.mh, st.Dir.Users)
	if err != nil {
		s.logger.Printf("mail sync: %v", err)
	}
	for slug, n := range added {
		s.logger.Printf("mail sync: %d new mail(s) for %s", n, slug)
		s.bus.Publish(events.Event{Kind: "mail", Slug: slug})
	}
}

// ---- view models ---------------------------------------------------------------------------------

var idRe = regexp.MustCompile(`[^A-Za-z0-9]`)

// Row is the per-user view model of the overview table.
type Row struct {
	*store.User
	ID           string // slug made safe for signal names / element ids
	State        string
	StateClass   string
	Job          *jobs.Job
	Running      bool
	CanOnboard   bool
	CanMesh      bool
	CanTerraform bool
	OTP          string
	OTPValid     int
	ProjectURL   string
	WorkspaceURL string
}

func (s *Server) row(u *store.User) Row {
	r := Row{User: u, ID: idRe.ReplaceAllString(u.Slug, "_")}
	r.State, r.StateClass = stateOf(u.Step)
	r.Job = s.jobs.Get(u.Slug)
	r.Running = r.Job != nil && r.Job.Running
	r.CanOnboard = !r.Running && !u.Step.Reached(store.StepOTPEnabled)
	r.CanMesh = !r.Running && u.Step.Reached(store.StepOTPEnabled) && !u.Step.Reached(store.StepServiceAccountCreated)
	r.CanTerraform = !r.Running && u.Step.Reached(store.StepServiceAccountCreated)
	r.ProjectURL = s.cfg.StackitProjectURL(u.Project.ID)
	r.WorkspaceURL = s.cfg.MeshstackWorkspaceURL(u.MeshstackWorkspace)
	if u.OTPSecret != "" {
		if code, valid, err := totp.Now(u.OTPSecret); err == nil {
			r.OTP, r.OTPValid = code, int(valid.Seconds())
		}
	}
	if r.Running {
		r.State = "running: " + r.Job.Name
		r.StateClass = "running"
	}
	return r
}

func stateOf(step store.Step) (string, string) {
	switch step {
	case store.StepCreated:
		return "new, not registered", "s0"
	case store.StepRegistered:
		return "in registration (awaiting e-mail)", "s1"
	case store.StepEmailVerified:
		return "e-mail verified, MFA pending", "s2"
	case store.StepOTPEnabled:
		return "ready (MFA enabled)", "s3"
	case store.StepProjectAssigned:
		return "ready, project assigned", "s4"
	case store.StepServiceAccountCreated:
		return "project + service account, terraform pending", "s5"
	case store.StepTerraformDeployed:
		return "complete (terraform deployed)", "s6"
	}
	return string(step), ""
}

func (s *Server) rows() ([]Row, error) {
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(st.Dir.Users, func(i, j int) bool { return st.Dir.Users[i].Slug < st.Dir.Users[j].Slug })
	out := make([]Row, 0, len(st.Dir.Users))
	for _, u := range st.Dir.Users {
		out = append(out, s.row(u))
	}
	return out, nil
}

// Detail is the view model of the user page.
type Detail struct {
	Row
	KeyJSON     string
	TFOutputs   string
	TFZip       bool
	Jumphost    *steps.Jumphost
	Mails       []mailstore.Entry
	Screenshots []string
	Dumps       []string
	Timeline    []timelineEntry
	MailSynced  bool
}

type timelineEntry struct {
	Step store.Step
	At   time.Time
}

func (s *Server) detail(slug string) (*Detail, error) {
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		return nil, err
	}
	u, err := st.Get(slug)
	if err != nil {
		return nil, err
	}
	d := &Detail{Row: s.row(u)}
	if u.Project.ServiceAccount.KeyFile != "" {
		if data, err := os.ReadFile(filepath.Join(s.cfg.Mesh.KeyDir, filepath.Base(u.Project.ServiceAccount.KeyFile))); err == nil {
			var pretty bytes.Buffer
			if json.Indent(&pretty, data, "", "  ") == nil {
				d.KeyJSON = pretty.String()
			} else {
				d.KeyJSON = string(data)
			}
		}
	}
	run := &steps.Runner{Cfg: s.cfg, Store: st}
	d.TFOutputs = run.TerraformOutputs(u)
	d.Jumphost = run.TerraformJumphost(u)
	if zp := run.TerraformZipPath(u); zp != "" {
		if _, err := os.Stat(zp); err == nil {
			d.TFZip = true
		}
	}
	d.Mails, _ = s.mails.List(slug)
	for _, step := range store.Order {
		if t, ok := u.Timeline[step]; ok {
			d.Timeline = append(d.Timeline, timelineEntry{Step: step, At: t})
		}
	}
	files, _ := os.ReadDir(s.cfg.Browser.ScreenshotDir)
	for _, f := range files {
		if !strings.HasPrefix(f.Name(), slug+"-") {
			continue
		}
		switch filepath.Ext(f.Name()) {
		case ".png":
			d.Screenshots = append(d.Screenshots, f.Name())
		case ".html":
			d.Dumps = append(d.Dumps, f.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(d.Screenshots)))
	sort.Sort(sort.Reverse(sort.StringSlice(d.Dumps)))
	return d, nil
}

// ---- pages ---------------------------------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	rows, err := s.rows()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "index", map[string]any{"Rows": rows, "Domain": s.cfg.CatchallDomain, "Auth": s.cfg.Web.AuthEnabled})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	ch, unsub := s.bus.Subscribe()
	defer unsub()
	push := func() {
		rows, err := s.rows()
		if err != nil {
			s.logger.Printf("events: %v", err)
			return
		}
		html, err := s.renderString("rows", map[string]any{"Rows": rows})
		if err != nil {
			s.logger.Printf("events: %v", err)
			return
		}
		_ = sse.PatchElements(html)
	}
	push()
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-sse.Context().Done():
			return
		case <-ch:
			// coalesce bursts (job log lines etc.)
			time.Sleep(100 * time.Millisecond)
		drain:
			for {
				select {
				case <-ch:
				default:
					break drain
				}
			}
			push()
		case <-keepalive.C:
			_ = sse.MarshalAndPatchSignals(map[string]any{"_hb": time.Now().Unix()})
		}
	}
}

type addSignals struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (s *Server) handleAddUser(w http.ResponseWriter, r *http.Request) {
	var in addSignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		s.formError(sse, err)
		return
	}
	run := &steps.Runner{Cfg: s.cfg, Store: st, Log: func(string, ...any) {}}
	u, err := run.Create(strings.TrimSpace(in.Slug), store.Participant{Name: strings.TrimSpace(in.Name), Email: strings.TrimSpace(in.Email)})
	if err != nil {
		s.formError(sse, err)
		return
	}
	s.bus.Publish(events.Event{Kind: "users", Slug: u.Slug})
	_ = sse.MarshalAndPatchSignals(addSignals{})
	_ = sse.PatchElements(`<div id="add-msg" class="ok">created ` + template.HTMLEscapeString(u.Username) + `</div>`)
}

func (s *Server) formError(sse *datastar.ServerSentEventGenerator, err error) {
	_ = sse.PatchElements(`<div id="add-msg" class="err">` + template.HTMLEscapeString(err.Error()) + `</div>`)
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	d, err := s.detail(r.PathValue("slug"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.render(w, "user", d)
}

func (s *Server) handleUserEvents(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	sse := datastar.NewSSE(w, r)
	ch, unsub := s.bus.Subscribe()
	defer unsub()
	push := func() {
		d, err := s.detail(slug)
		if err != nil {
			return
		}
		html, err := s.renderString("detail", d)
		if err != nil {
			s.logger.Printf("user events: %v", err)
			return
		}
		_ = sse.PatchElements(html)
	}
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-sse.Context().Done():
			return
		case e := <-ch:
			if e.Slug != "" && e.Slug != slug {
				continue
			}
			time.Sleep(100 * time.Millisecond)
		drain:
			for {
				select {
				case <-ch:
				default:
					break drain
				}
			}
			push()
		case <-keepalive.C:
			_ = sse.MarshalAndPatchSignals(map[string]any{"_hb": time.Now().Unix()})
		}
	}
}

// handleOTPs streams the current TOTP codes (with remaining validity) once per second:
// all users' table cells (default), or one user's element (?slug=&tag=&target=).
func (s *Server) handleOTPs(w http.ResponseWriter, r *http.Request) {
	slug, tag, target := r.URL.Query().Get("slug"), r.URL.Query().Get("tag"), r.URL.Query().Get("target")
	if tag != "span" {
		tag = "td"
	}
	sse := datastar.NewSSE(w, r)
	for {
		st, err := store.Load(s.cfg.UsersFile)
		if err != nil {
			return
		}
		var sb strings.Builder
		for _, u := range st.Dir.Users {
			if slug != "" && u.Slug != slug {
				continue
			}
			if u.OTPSecret == "" {
				continue
			}
			code, valid, err := totp.Now(u.OTPSecret)
			if err != nil {
				continue
			}
			id := target
			if id == "" {
				id = "otp-" + idRe.ReplaceAllString(u.Slug, "_")
			}
			html, err := s.renderString("otpcell", map[string]any{"ID": id, "Tag": tag, "Code": code, "Valid": int(valid.Seconds()), "HasSecret": true})
			if err != nil {
				s.logger.Printf("otps: %v", err)
				return
			}
			sb.WriteString(html)
			sb.WriteString("\n")
		}
		if sb.Len() > 0 {
			if err := sse.PatchElements(sb.String()); err != nil {
				return
			}
		}
		select {
		case <-sse.Context().Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// handleTerraformZip serves the zipped terraform project for download.
func (s *Server) handleTerraformZip(w http.ResponseWriter, r *http.Request) {
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u, err := st.Get(r.PathValue("slug"))
	if err != nil || u.Terraform.Zip == "" {
		http.NotFound(w, r)
		return
	}
	run := &steps.Runner{Cfg: s.cfg, Store: st}
	w.Header().Set("Content-Disposition", "attachment; filename=\"terraform-"+u.Slug+".zip\"")
	http.ServeFile(w, r, run.TerraformZipPath(u))
}

// handleSAKey serves the stored service account key JSON for download.
func (s *Server) handleSAKey(w http.ResponseWriter, r *http.Request) {
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u, err := st.Get(r.PathValue("slug"))
	if err != nil || u.Project.ServiceAccount.KeyFile == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+u.Slug+"-sa-key.json\"")
	http.ServeFile(w, r, filepath.Join(s.cfg.Mesh.KeyDir, filepath.Base(u.Project.ServiceAccount.KeyFile)))
}

// handleRun starts an account-creation step as a background job.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	slug, action := r.PathValue("slug"), r.PathValue("action")
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := st.Get(slug); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	force := r.URL.Query().Get("force") == "1"
	var fn func(run *steps.Runner, u *store.User, ctx context.Context) error
	switch action {
	case "onboard":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.Onboard(ctx, u) }
	case "register":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.Register(u, force) }
	case "verify-email":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error {
			return run.VerifyEmail(ctx, u, force, false)
		}
	case "resend-verification":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error {
			return run.VerifyEmail(ctx, u, force, true)
		}
	case "setup-otp":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.SetupOTP(u, force) }
	case "mesh-project":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.MeshProject(u, force) }
	case "service-account":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.ServiceAccount(u, force) }
	case "mesh-onboard":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.MeshOnboard(u) }
	case "terraform":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error {
			return run.TerraformDeploy(ctx, u, force)
		}
	case "terraform-zip":
		fn = func(run *steps.Runner, u *store.User, ctx context.Context) error { return run.TerraformZip(u) }
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	err = s.jobs.Start(slug, action, func(logf func(string, ...any)) error {
		st, err := store.Load(s.cfg.UsersFile)
		if err != nil {
			return err
		}
		u, err := st.Get(slug)
		if err != nil {
			return err
		}
		run := &steps.Runner{Cfg: s.cfg, Store: st, Log: logf, Headless: s.headless}
		return fn(run, u, context.Background())
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElements(`<div id="add-msg" class="err">` + template.HTMLEscapeString(err.Error()) + `</div>`)
		return
	}
	s.bus.Publish(events.Event{Kind: "job", Slug: slug})
}

type participantSignals struct {
	Name  string `json:"pname"`
	Email string `json:"pemail"`
}

func (s *Server) handleParticipant(w http.ResponseWriter, r *http.Request) {
	var in participantSignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.updateUser(w, r, func(u *store.User) {
		u.Participant = store.Participant{Name: strings.TrimSpace(in.Name), Email: strings.TrimSpace(in.Email)}
		u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	})
}

type projectSignals struct {
	Project string `json:"project"`
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	var in projectSignals
	if err := datastar.ReadSignals(r, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.updateUser(w, r, func(u *store.User) {
		u.Project.ID = strings.TrimSpace(in.Project)
		if u.Project.ID != "" {
			u.Advance(store.StepProjectAssigned)
		} else {
			u.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		}
	})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, mutate func(*store.User)) {
	slug := r.PathValue("slug")
	st, err := store.Load(s.cfg.UsersFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u, err := st.Get(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	mutate(u)
	if err := st.Save(u); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.bus.Publish(events.Event{Kind: "users", Slug: slug})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMailSync(w http.ResponseWriter, r *http.Request) {
	s.syncMail(r.Context())
	s.bus.Publish(events.Event{Kind: "mail", Slug: r.PathValue("slug")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	e, err := s.mails.Get(slug, r.PathValue("file"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	headers := make([]string, 0, len(e.Message.Content.Headers))
	for k := range e.Message.Content.Headers {
		headers = append(headers, k)
	}
	sort.Strings(headers)
	s.render(w, "mail", map[string]any{
		"Slug": slug, "Entry": e, "Headers": headers,
		"Text": e.Message.Part("text/plain"), "HasHTML": e.Message.Part("text/html") != "",
	})
}

func (s *Server) handleMailHTML(w http.ResponseWriter, r *http.Request) {
	e, err := s.mails.Get(r.PathValue("slug"), r.PathValue("file"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src https: data:; style-src 'unsafe-inline'")
	_, _ = w.Write([]byte(e.Message.Part("text/html")))
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name != filepath.Base(name) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.cfg.Browser.ScreenshotDir, name))
}

// ---- templates -----------------------------------------------------------------------------------

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"jsq": func(v string) string { b, _ := json.Marshal(v); return string(b) },
		// raw inserts an already-safe identifier (e.g. Row.ID, [A-Za-z0-9_] only) unescaped into JS contexts.
		"raw": func(v string) template.JS { return template.JS(idRe.ReplaceAllString(v, "_")) },
		"time": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Local().Format("2006-01-02 15:04:05")
		},
		"mask": func(v string) string { return strings.Repeat("•", len(v)) },
		"join": strings.Join,
		"lines": func(j *jobs.Job, n int) []string {
			if j == nil {
				return nil
			}
			if len(j.Lines) > n {
				return j.Lines[len(j.Lines)-n:]
			}
			return j.Lines
		},
		"stepReached": func(u *store.User, step string) bool { return u.Step.Reached(store.Step(step)) },
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Printf("render %s: %v", name, err)
	}
}

func (s *Server) renderString(name string, data any) (string, error) {
	var sb strings.Builder
	if err := s.tmpl.ExecuteTemplate(&sb, name, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}
