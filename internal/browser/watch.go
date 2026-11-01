package browser

import (
	"strings"
	"time"
)

// Watch logs in via setupURL and then reports URL, headings and buttons every interval for the given
// duration (debugging aid for discovering a changed third-party flow). It never clicks anything except
// the cookie banner.
func (s *Session) Watch(setupURL, username, password string, codeFor func() (string, error), duration, interval time.Duration) error {
	log := s.opts.Log
	p := s.Page
	if _, err := p.Goto(setupURL); err != nil {
		return s.fail("watch-goto", err)
	}
	if strings.Contains(p.URL(), "accounts.stackit.cloud") {
		if err := s.LoginIDP(username, password, codeFor); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(duration)
	last := ""
	for time.Now().Before(deadline) {
		if b := s.button("Decline"); isVisible(b) {
			_ = b.Click()
		}
		heads, _ := p.Locator("h1, h2, h3").AllInnerTexts()
		buttons, _ := p.Locator("button, a[role=button]").AllInnerTexts()
		var bs []string
		for _, b := range buttons {
			if b = strings.TrimSpace(b); b != "" {
				bs = append(bs, b)
			}
		}
		var hs []string
		for _, h := range heads {
			if h = strings.TrimSpace(h); h != "" {
				hs = append(hs, h)
			}
		}
		state := p.URL() + " | " + strings.Join(hs, " / ") + " | " + strings.Join(bs, ", ")
		if state != last {
			last = state
			log("url=%s\n   headings: %s\n   buttons: %s", p.URL(), strings.Join(hs, " / "), strings.Join(bs, ", "))
			s.Screenshot("watch")
		}
		time.Sleep(interval)
	}
	return nil
}
