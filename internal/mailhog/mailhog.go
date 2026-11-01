// Package mailhog is a minimal client for the MailHog v2 HTTP API.
package mailhog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to one MailHog instance.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New creates a client for baseURL (e.g. https://mailhog.mentala.de).
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Path is a MailHog address.
type Path struct {
	Mailbox string `json:"Mailbox"`
	Domain  string `json:"Domain"`
}

func (p Path) String() string { return p.Mailbox + "@" + p.Domain }

// Content is a MailHog MIME body (used for the top-level and for each part).
type Content struct {
	Headers map[string][]string `json:"Headers"`
	Body    string              `json:"Body"`
	MIME    *MIMEBody           `json:"MIME"`
}

// MIMEBody holds the parsed MIME parts.
type MIMEBody struct {
	Parts []Content `json:"Parts"`
}

// Message is one stored mail.
type Message struct {
	ID      string    `json:"ID"`
	From    Path      `json:"From"`
	To      []Path    `json:"To"`
	Created time.Time `json:"Created"`
	Content Content   `json:"Content"`
	MIME    *MIMEBody `json:"MIME"`
}

type listResponse struct {
	Total int       `json:"total"`
	Count int       `json:"count"`
	Items []Message `json:"items"`
}

func (m *Message) header(name string) string {
	if v, ok := m.Content.Headers[name]; ok && len(v) > 0 {
		dec := new(mime.WordDecoder)
		if s, err := dec.DecodeHeader(v[0]); err == nil {
			return s
		}
		return v[0]
	}
	return ""
}

// Subject returns the decoded subject.
func (m *Message) Subject() string { return m.header("Subject") }

// FromHeader returns the From header (display form, e.g. `"STACKIT Accounts" <info@...>`).
func (m *Message) FromHeader() string { return m.header("From") }

// FromAddress returns the bare sender address from the From header, falling back to the envelope sender.
func (m *Message) FromAddress() string {
	h := m.FromHeader()
	if i := strings.LastIndex(h, "<"); i >= 0 {
		if j := strings.Index(h[i:], ">"); j > 0 {
			return strings.ToLower(h[i+1 : i+j])
		}
	}
	if h != "" {
		return strings.ToLower(strings.TrimSpace(h))
	}
	return strings.ToLower(m.From.String())
}

// IsTo reports whether addr is among the recipients.
func (m *Message) IsTo(addr string) bool {
	addr = strings.ToLower(addr)
	for _, t := range m.To {
		if strings.ToLower(t.String()) == addr {
			return true
		}
	}
	return false
}

// Text returns the decoded textual content of all parts (text/plain and text/html), concatenated.
func (m *Message) Text() string {
	var sb strings.Builder
	var walk func(c Content)
	walk = func(c Content) {
		if c.MIME != nil && len(c.MIME.Parts) > 0 {
			for _, p := range c.MIME.Parts {
				walk(p)
			}
			return
		}
		if c.Body == "" {
			return
		}
		sb.WriteString(decodeBody(c))
		sb.WriteString("\n")
	}
	if m.MIME != nil && len(m.MIME.Parts) > 0 {
		for _, p := range m.MIME.Parts {
			walk(p)
		}
	} else {
		walk(m.Content)
	}
	return sb.String()
}

// Part returns the decoded body of the first part whose Content-Type starts with ctype ("text/html", "text/plain").
func (m *Message) Part(ctype string) string {
	var found string
	var walk func(c Content) bool
	walk = func(c Content) bool {
		if c.MIME != nil && len(c.MIME.Parts) > 0 {
			for _, p := range c.MIME.Parts {
				if walk(p) {
					return true
				}
			}
			return false
		}
		ct := ""
		if v, ok := c.Headers["Content-Type"]; ok && len(v) > 0 {
			ct = strings.ToLower(v[0])
		}
		if strings.HasPrefix(ct, ctype) && c.Body != "" {
			found = decodeBody(c)
			return true
		}
		return false
	}
	if m.MIME != nil && len(m.MIME.Parts) > 0 {
		for _, p := range m.MIME.Parts {
			if walk(p) {
				break
			}
		}
	} else {
		c := m.Content
		ct := ""
		if v, ok := c.Headers["Content-Type"]; ok && len(v) > 0 {
			ct = strings.ToLower(v[0])
		}
		if strings.HasPrefix(ct, ctype) || ctype == "text/plain" {
			found = decodeBody(c)
		}
	}
	return found
}

func decodeBody(c Content) string {
	enc := ""
	if v, ok := c.Headers["Content-Transfer-Encoding"]; ok && len(v) > 0 {
		enc = strings.ToLower(strings.TrimSpace(v[0]))
	}
	switch enc {
	case "quoted-printable":
		b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(c.Body)))
		if err == nil {
			return string(b)
		}
		return c.Body
	case "base64":
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(c.Body), ""))
		if err == nil {
			return string(b)
		}
		return c.Body
	default:
		return c.Body
	}
}

// Search queries /api/v2/search. kind is "to", "from" or "containing".
func (c *Client) Search(ctx context.Context, kind, query string, limit int) ([]Message, error) {
	u := fmt.Sprintf("%s/api/v2/search?kind=%s&query=%s&limit=%d", c.BaseURL, url.QueryEscape(kind), url.QueryEscape(query), limit)
	return c.list(ctx, u)
}

// SearchTo returns all messages addressed exactly to addr (case-insensitive), newest first.
// MailHog's "to" search is a substring match (a query for 1@example.org also matches 11@example.org
// and 21@example.org) and the result is paginated, so this pages through all matches and keeps only
// the exact recipient. max limits the number of returned mails (0 = all).
func (c *Client) SearchTo(ctx context.Context, addr string, max int) ([]Message, error) {
	const page = 200
	var out []Message
	for start := 0; ; start += page {
		u := fmt.Sprintf("%s/api/v2/search?kind=to&query=%s&start=%d&limit=%d", c.BaseURL, url.QueryEscape(addr), start, page)
		lr, err := c.fetch(ctx, u)
		if err != nil {
			return nil, err
		}
		for i := range lr.Items {
			if lr.Items[i].IsTo(addr) {
				out = append(out, lr.Items[i])
				if max > 0 && len(out) >= max {
					return out, nil
				}
			}
		}
		if len(lr.Items) == 0 || start+len(lr.Items) >= lr.Total {
			return out, nil
		}
	}
}

// Messages lists the newest messages (/api/v2/messages).
func (c *Client) Messages(ctx context.Context, limit int) ([]Message, error) {
	return c.list(ctx, fmt.Sprintf("%s/api/v2/messages?limit=%d", c.BaseURL, limit))
}

func (c *Client) list(ctx context.Context, u string) ([]Message, error) {
	lr, err := c.fetch(ctx, u)
	if err != nil {
		return nil, err
	}
	return lr.Items, nil
}

func (c *Client) fetch(ctx context.Context, u string) (*listResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mailhog request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("mailhog %s: HTTP %d: %s", u, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var lr listResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, fmt.Errorf("decode mailhog response: %w", err)
	}
	return &lr, nil
}

// Matcher decides whether a message is the one we wait for.
type Matcher func(*Message) bool

// WaitFor polls the mailbox of `to` until a message matching match (and created after since) shows up.
// onPoll (optional) is called after every poll with the number of candidate mails seen.
func (c *Client) WaitFor(ctx context.Context, to string, since time.Time, interval time.Duration, match Matcher) (*Message, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		msgs, err := c.SearchTo(ctx, to, 0)
		if err != nil {
			return nil, err
		}
		for i := range msgs {
			m := &msgs[i]
			if m.Created.Before(since) {
				continue
			}
			if match(m) {
				return m, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for mail to %s: %w", to, ctx.Err())
		case <-ticker.C:
		}
	}
}

// SubjectFrom builds a matcher for a sender address and a subject substring.
func SubjectFrom(from, subjectContains string) Matcher {
	from = strings.ToLower(from)
	return func(m *Message) bool {
		return m.FromAddress() == from && strings.Contains(m.Subject(), subjectContains)
	}
}
