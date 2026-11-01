// Package mailstore archives the MailHog messages of every user on local disk
// (<mail_dir>/<slug>/<timestamp>-<hash>.json, raw MailHog JSON) so the history survives
// MailHog purges and can be shown in the web UI.
package mailstore

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/denktmit/stackit-ws-manager/internal/mailhog"
	"github.com/denktmit/stackit-ws-manager/internal/store"
)

// Store is the local mail archive.
type Store struct {
	Dir string
}

// Entry is one archived mail.
type Entry struct {
	File    string // base file name (used as id in URLs)
	Message mailhog.Message
}

// idHash is the stable part of a file name: a hash of the MailHog message id.
func idHash(m *mailhog.Message) string {
	sum := sha1.Sum([]byte(m.ID))
	return hex.EncodeToString(sum[:6])
}

func fileFor(m *mailhog.Message) string {
	return fmt.Sprintf("%s-%s.json", m.Created.UTC().Format("20060102-150405"), idHash(m))
}

// hashOf extracts the id hash from an archive file name (<timestamp>-<hash>.json), or "".
func hashOf(name string) string {
	name = strings.TrimSuffix(name, ".json")
	i := strings.LastIndex(name, "-")
	if i < 0 || i == len(name)-1 {
		return ""
	}
	return name[i+1:]
}

// archived returns the id hashes already present in dir.
func archived(dir string) (map[string]bool, error) {
	seen := map[string]bool{}
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return seen, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if h := hashOf(f.Name()); h != "" && !f.IsDir() {
			seen[h] = true
		}
	}
	return seen, nil
}

// Dedupe removes stale .json.tmp files and archive files that refer to the same MailHog message
// (same id hash), keeping the oldest file of each. Some sinks report a moving "Created" time, which used to produce a new
// file per sync. Returns the number of removed files per slug.
func (s *Store) Dedupe() (map[string]int, error) {
	removed := map[string]int{}
	slugs, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return removed, nil
	}
	if err != nil {
		return nil, err
	}
	for _, d := range slugs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(s.Dir, d.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			return removed, err
		}
		names := make([]string, 0, len(files))
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if strings.HasSuffix(f.Name(), ".json.tmp") { // leftover of an interrupted write
				if err := os.Remove(filepath.Join(dir, f.Name())); err != nil {
					return removed, err
				}
				removed[d.Name()]++
				continue
			}
			if strings.HasSuffix(f.Name(), ".json") {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names) // timestamp prefix: oldest first
		seen := map[string]bool{}
		for _, n := range names {
			h := hashOf(n)
			if h == "" {
				continue
			}
			if seen[h] {
				if err := os.Remove(filepath.Join(dir, n)); err != nil {
					return removed, err
				}
				removed[d.Name()]++
				continue
			}
			seen[h] = true
		}
	}
	return removed, nil
}

// Sync copies all mails addressed to the users into the archive. Returns the number of new mails per slug.
func (s *Store) Sync(ctx context.Context, mh *mailhog.Client, users []*store.User) (map[string]int, error) {
	added := map[string]int{}
	for _, u := range users {
		// exact-recipient search: the slug alone (e.g. "1") or even the address as a substring would
		// match other users' mailboxes
		msgs, err := mh.SearchTo(ctx, u.Username, 0)
		if err != nil {
			return added, fmt.Errorf("sync %s: %w", u.Slug, err)
		}
		dir := filepath.Join(s.Dir, u.Slug)
		seen, err := archived(dir)
		if err != nil {
			return added, err
		}
		for i := range msgs {
			m := &msgs[i]
			if seen[idHash(m)] {
				continue
			}
			seen[idHash(m)] = true
			path := filepath.Join(dir, fileFor(m))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return added, err
			}
			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return added, err
			}
			if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
				os.Remove(path + ".tmp") // e.g. disk full: do not leave empty files behind
				return added, err
			}
			if err := os.Rename(path+".tmp", path); err != nil {
				os.Remove(path + ".tmp")
				return added, err
			}
			added[u.Slug]++
		}
	}
	return added, nil
}

// List returns the archived mails of a user, newest first.
func (s *Store) List(slug string) ([]Entry, error) {
	dir := filepath.Join(s.Dir, slug)
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		e, err := s.Get(slug, f.Name())
		if err != nil {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Message.Created.After(out[j].Message.Created) })
	return out, nil
}

// Get loads one archived mail.
func (s *Store) Get(slug, file string) (*Entry, error) {
	if file != filepath.Base(file) || !strings.HasSuffix(file, ".json") {
		return nil, fmt.Errorf("invalid mail id %q", file)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, slug, file))
	if err != nil {
		return nil, err
	}
	var m mailhog.Message
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &Entry{File: file, Message: m}, nil
}
