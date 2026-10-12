// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package schedule evaluates unions of cron windows without running background jobs.
package schedule

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // Distroless images need the IANA timezone database.

	"github.com/robfig/cron/v3"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

const MaxDuration = 7 * 24 * time.Hour

type (
	entry struct {
		cron     cron.Schedule
		duration time.Duration
	}
	Window struct {
		Active     bool
		Start, End time.Time
	}
	Engine struct {
		entries     []entry
		mu          sync.Mutex
		cached      Window
		from, until time.Time
		windows     []Window
	}
)

func Compile(schedules []api.Schedule) (*Engine, error) {
	if len(schedules) == 0 || len(schedules) > 16 {
		return nil, fmt.Errorf("require 1 to 16 schedules")
	}
	e := &Engine{}
	names := map[string]bool{}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, s := range schedules {
		if s.Name == "" || names[s.Name] {
			return nil, fmt.Errorf("schedule names must be nonempty and unique")
		}
		names[s.Name] = true
		if s.Duration.Duration <= 0 || s.Duration.Duration > MaxDuration {
			return nil, fmt.Errorf("schedule %s duration must be positive and <=168h", s.Name)
		}
		if len(strings.Fields(s.Cron)) != 5 {
			return nil, fmt.Errorf("schedule %s requires five cron fields", s.Name)
		}
		zone := s.TimeZone
		if zone == "" {
			zone = "UTC"
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return nil, fmt.Errorf("timeZone: %w", err)
		}
		c, err := parser.Parse("CRON_TZ=" + zone + " " + s.Cron)
		if err != nil {
			return nil, fmt.Errorf("schedule %s: %w", s.Name, err)
		}
		e.entries = append(e.entries, entry{c, s.Duration.Duration})
	}
	return e, nil
}

// At caches upcoming window boundaries for one hour. Work and
// memory are bounded by 16 schedules and a 28-day horizon, including lookback.
// A zero End denotes a continuous union extending beyond the lookahead horizon.
func (e *Engine) At(now time.Time) Window {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !now.Before(e.from) && now.Before(e.until) {
		return e.lookup(now)
	}
	horizon := now.Add(2 * MaxDuration)
	var intervals []Window
	for _, s := range e.entries {
		var merged Window
		for start := s.cron.Next(now.Add(-2 * MaxDuration).Add(-time.Nanosecond)); !start.IsZero() && !start.After(horizon); start = s.cron.Next(start) {
			end := start.Add(s.duration)
			if merged.Start.IsZero() {
				merged = Window{Start: start, End: end}
				continue
			}
			if !start.After(merged.End) {
				if end.After(merged.End) {
					merged.End = end
				}
				continue
			}
			intervals = append(intervals, merged)
			merged = Window{Start: start, End: end}
		}
		if !merged.Start.IsZero() {
			intervals = append(intervals, merged)
		}
		// Sparse schedules still need a next start beyond the lookahead horizon.
		if merged.Start.IsZero() || !merged.End.After(now) {
			if start := s.cron.Next(now); !start.IsZero() {
				intervals = append(intervals, Window{Start: start, End: start.Add(s.duration)})
			}
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start.Before(intervals[j].Start) })

	var merged []Window
	for _, candidate := range intervals {
		if len(merged) == 0 || candidate.Start.After(merged[len(merged)-1].End) {
			merged = append(merged, candidate)
			continue
		}
		if candidate.End.After(merged[len(merged)-1].End) {
			merged[len(merged)-1].End = candidate.End
		}
	}
	e.from = now
	e.until = now.Add(time.Hour)
	e.windows = nil
	for _, w := range merged {
		if !w.End.After(now) {
			continue
		}
		if !w.Start.After(horizon) && w.End.After(horizon) {
			w.End = time.Time{}
		}
		if !now.Before(w.Start) && e.cached.Active && (e.cached.End.IsZero() || now.Before(e.cached.End)) && e.cached.Start.Before(w.Start) {
			w.Start = e.cached.Start
		}
		e.windows = append(e.windows, w)
		// Keep at most one hour of boundaries and the first following window.
		if w.Start.After(e.until) || w.End.IsZero() {
			break
		}
	}
	w := e.lookup(now)
	e.cached = w
	return w
}

func (e *Engine) lookup(now time.Time) Window {
	i := sort.Search(len(e.windows), func(i int) bool { return e.windows[i].End.IsZero() || e.windows[i].End.After(now) })
	if i == len(e.windows) {
		return Window{}
	}
	w := e.windows[i]
	w.Active = !now.Before(w.Start) && (w.End.IsZero() || now.Before(w.End))
	return w
}
