// Package browser wraps a headless Playwright Chromium session and the STACKIT IDP flows.
package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// Options configures a Session.
type Options struct {
	Headless      bool
	SlowMo        time.Duration
	Timeout       time.Duration
	Locale        string
	ScreenshotDir string
	// Prefix (e.g. the user slug) is prepended to screenshot / html dump file names.
	Prefix string
	// Log receives progress lines (optional).
	Log func(format string, args ...any)
}

// Session is one browser context with a single page.
type Session struct {
	opts    Options
	pw      *playwright.Playwright
	browser playwright.Browser
	ctx     playwright.BrowserContext
	Page    playwright.Page
	Expect  playwright.PlaywrightAssertions
}

// Install downloads the Playwright driver and Chromium matching the Go library version.
// withDeps additionally installs the OS packages Chromium needs (`playwright install --with-deps`).
func Install(withDeps bool) error {
	return playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}, WithDeps: withDeps})
}

// Check launches headless Chromium once and returns its version (installation smoke test).
func Check() (string, error) {
	s, err := New(Options{Headless: true, Timeout: 30 * time.Second})
	if err != nil {
		return "", err
	}
	defer s.Close()
	if _, err := s.Page.Goto("about:blank"); err != nil {
		return "", err
	}
	return s.browser.Version(), nil
}

// New launches Chromium and opens a fresh (cookie-less) context + page.
func New(opts Options) (*Session, error) {
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Locale == "" {
		opts.Locale = "de-DE"
	}
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright (run `browser install` first?): %w", err)
	}
	b, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(opts.Headless),
		SlowMo:   playwright.Float(float64(opts.SlowMo.Milliseconds())),
	})
	if err != nil {
		pw.Stop()
		return nil, fmt.Errorf("launch chromium: %w", err)
	}
	ctx, err := b.NewContext(playwright.BrowserNewContextOptions{
		Locale:   playwright.String(opts.Locale),
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	})
	if err != nil {
		b.Close()
		pw.Stop()
		return nil, fmt.Errorf("new browser context: %w", err)
	}
	ctx.SetDefaultTimeout(float64(opts.Timeout.Milliseconds()))
	page, err := ctx.NewPage()
	if err != nil {
		ctx.Close()
		b.Close()
		pw.Stop()
		return nil, fmt.Errorf("new page: %w", err)
	}
	return &Session{
		opts:    opts,
		pw:      pw,
		browser: b,
		ctx:     ctx,
		Page:    page,
		Expect:  playwright.NewPlaywrightAssertions(float64(opts.Timeout.Milliseconds())),
	}, nil
}

// Close tears everything down.
func (s *Session) Close() {
	if s == nil {
		return
	}
	_ = s.ctx.Close()
	_ = s.browser.Close()
	_ = s.pw.Stop()
}

func (s *Session) fileName(name, ext string) string {
	ts := time.Now().Format("20060102-150405")
	if s.opts.Prefix != "" {
		return fmt.Sprintf("%s-%s-%s.%s", s.opts.Prefix, name, ts, ext)
	}
	return fmt.Sprintf("%s-%s.%s", name, ts, ext)
}

// Screenshot stores a full-page PNG named after `name` and returns its path (empty on failure).
func (s *Session) Screenshot(name string) string {
	if s.opts.ScreenshotDir == "" {
		return ""
	}
	if err := os.MkdirAll(s.opts.ScreenshotDir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(s.opts.ScreenshotDir, s.fileName(name, "png"))
	if _, err := s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(path), FullPage: playwright.Bool(true)}); err != nil {
		return ""
	}
	return path
}

// DumpHTML stores the page body HTML next to the screenshots (debugging aid).
func (s *Session) DumpHTML(name string) string {
	if s.opts.ScreenshotDir == "" {
		return ""
	}
	html, err := s.Page.Locator("body").InnerHTML()
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(s.opts.ScreenshotDir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(s.opts.ScreenshotDir, s.fileName(name, "html"))
	if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
		return ""
	}
	return path
}

// fail wraps err with the current URL and a screenshot for debugging.
func (s *Session) fail(name string, err error) error {
	shot := s.Screenshot(name)
	url := s.Page.URL()
	if shot != "" {
		return fmt.Errorf("%w (url=%s, screenshot=%s)", err, url, shot)
	}
	return fmt.Errorf("%w (url=%s)", err, url)
}

// errorMessage returns the visible IDP error banner text, if any.
func (s *Session) errorMessage() string {
	// Zitadel login UI: field errors live in `.lgn-error` containers (`.info-msg` ones are mere hints),
	// server-side errors are rendered as toasts.
	for _, sel := range []string{".lgn-toast.toast-error .lgn-toast-message", ".lgn-error:not(.info-msg) .lgn-error-message"} {
		loc := s.Page.Locator(sel).First()
		if visible, err := loc.IsVisible(); err == nil && visible {
			txt, _ := loc.InnerText()
			if t := strings.TrimSpace(txt); t != "" {
				return t
			}
		}
	}
	return ""
}

// ErrAlreadyRegistered is returned when the IDP already knows the username.
var ErrAlreadyRegistered = errors.New("user is already registered at STACKIT")

// role/textbox helpers keep the flows readable.
func (s *Session) button(name string) playwright.Locator {
	return s.Page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: name})
}

func (s *Session) textbox(name string, exact bool) playwright.Locator {
	return s.Page.GetByRole(*playwright.AriaRoleTextbox, playwright.PageGetByRoleOptions{Name: name, Exact: playwright.Bool(exact)})
}
