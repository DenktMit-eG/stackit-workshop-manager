// Package jobs runs long account-creation steps in the background (for the web UI) and keeps their logs.
package jobs

import (
	"fmt"
	"sync"
	"time"

	"github.com/denktmit/stackit-ws-manager/internal/events"
)

// Job is one background run for one user.
type Job struct {
	Slug     string
	Name     string
	Started  time.Time
	Finished time.Time
	Running  bool
	Err      string
	Lines    []string
}

// Manager allows at most one running job per user.
type Manager struct {
	mu   sync.Mutex
	jobs map[string]*Job // last job per slug
	bus  *events.Bus
}

// New creates a Manager publishing progress on bus.
func New(bus *events.Bus) *Manager {
	return &Manager{jobs: map[string]*Job{}, bus: bus}
}

// Get returns the last job of a user (a copy), or nil.
func (m *Manager) Get(slug string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[slug]
	if !ok {
		return nil
	}
	c := *j
	c.Lines = append([]string(nil), j.Lines...)
	return &c
}

// Running reports whether a job for slug is in progress.
func (m *Manager) Running(slug string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[slug]
	return ok && j.Running
}

// Start runs fn in a goroutine. logf passed to fn appends to the job log and notifies the bus.
func (m *Manager) Start(slug, name string, fn func(logf func(format string, args ...any)) error) error {
	m.mu.Lock()
	if j, ok := m.jobs[slug]; ok && j.Running {
		m.mu.Unlock()
		return fmt.Errorf("a job (%s) is already running for %s", j.Name, slug)
	}
	j := &Job{Slug: slug, Name: name, Started: time.Now(), Running: true}
	m.jobs[slug] = j
	m.mu.Unlock()

	logf := func(format string, args ...any) {
		line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
		m.mu.Lock()
		j.Lines = append(j.Lines, line)
		m.mu.Unlock()
		m.bus.Publish(events.Event{Kind: "job", Slug: slug})
	}
	go func() {
		err := fn(logf)
		m.mu.Lock()
		j.Running = false
		j.Finished = time.Now()
		if err != nil {
			j.Err = err.Error()
			j.Lines = append(j.Lines, time.Now().Format("15:04:05")+" ERROR: "+err.Error())
		} else {
			j.Lines = append(j.Lines, time.Now().Format("15:04:05")+" done")
		}
		m.mu.Unlock()
		m.bus.Publish(events.Event{Kind: "job", Slug: slug})
		m.bus.Publish(events.Event{Kind: "users", Slug: slug})
	}()
	return nil
}
