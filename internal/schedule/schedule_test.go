// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func TestWindows(t *testing.T) {
	specs := []api.Schedule{
		{Name: "first", Cron: "0 10 * * *", Duration: metav1.Duration{Duration: 2 * time.Hour}},
		{Name: "overlap", Cron: "0 11 * * *", Duration: metav1.Duration{Duration: 2 * time.Hour}},
		{Name: "adjacent", Cron: "0 13 * * *", Duration: metav1.Duration{Duration: time.Hour}},
	}
	for _, tc := range []struct {
		name, now, start, end string
		active                bool
	}{
		{"before", "2026-10-01T09:00:00Z", "2026-10-01T10:00:00Z", "2026-10-01T14:00:00Z", false},
		{"inclusive start", "2026-10-01T10:00:00Z", "2026-10-01T10:00:00Z", "2026-10-01T14:00:00Z", true},
		{"overlap", "2026-10-01T12:30:00Z", "2026-10-01T10:00:00Z", "2026-10-01T14:00:00Z", true},
		{"exclusive end", "2026-10-01T14:00:00Z", "2026-10-02T10:00:00Z", "2026-10-02T14:00:00Z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Compile(specs)
			if err != nil {
				t.Fatal(err)
			}
			w := e.At(parse(tc.now))
			if w.Active != tc.active || !w.Start.Equal(parse(tc.start)) || !w.End.Equal(parse(tc.end)) {
				t.Fatalf("unexpected window %+v", w)
			}
		})
	}
}

func TestContinuousAndDST(t *testing.T) {
	e, err := Compile([]api.Schedule{{Name: "continuous", Cron: "* * * * *", Duration: metav1.Duration{Duration: time.Minute}}})
	if err != nil {
		t.Fatal(err)
	}
	w := e.At(parse("2026-10-01T10:00:00Z"))
	if !w.Active || !w.End.IsZero() {
		t.Fatalf("continuous union: %+v", w)
	}
	e, err = Compile([]api.Schedule{{Name: "dst", Cron: "30 2 * * *", TimeZone: "Europe/Zurich", Duration: metav1.Duration{Duration: time.Hour}}})
	if err != nil {
		t.Fatal(err)
	}
	// Spring-forward skips a nonexistent local time.
	w = e.At(parse("2026-03-29T00:45:00Z"))
	if !w.Start.Equal(parse("2026-03-30T00:30:00Z")) {
		t.Fatalf("DST spring: %+v", w)
	}
	// Fall-back occurrences overlap/abut and remain one effective window.
	w = e.At(parse("2026-10-25T00:45:00Z"))
	if !w.Active || !w.End.Equal(parse("2026-10-25T02:30:00Z")) {
		t.Fatalf("DST fall: %+v", w)
	}
}

func TestInvalid(t *testing.T) {
	for _, s := range []api.Schedule{
		{Name: "bad", Cron: "@every 1m", Duration: metav1.Duration{Duration: time.Minute}},
		{Name: "bad", Cron: "0 0 * * *", TimeZone: "Mars/Olympus", Duration: metav1.Duration{Duration: time.Minute}},
		{Name: "bad", Cron: "0 0 * * *"},
		{Name: "bad", Cron: "0 0 * * *", Duration: metav1.Duration{Duration: MaxDuration + time.Second}},
	} {
		if _, err := Compile([]api.Schedule{s}); err == nil {
			t.Fatalf("accepted invalid %+v", s)
		}
	}
}

func TestCacheBoundaryAndClockReversal(t *testing.T) {
	e, _ := Compile([]api.Schedule{{Name: "a", Cron: "0 * * * *", Duration: metav1.Duration{Duration: 10 * time.Second}}})
	for _, tc := range []struct {
		at     string
		active bool
	}{{"2026-10-01T10:00:05Z", true}, {"2026-10-01T10:00:10Z", false}, {"2026-10-01T10:00:01Z", true}} {
		if w := e.At(parse(tc.at)); w.Active != tc.active {
			t.Fatalf("%s: %+v", tc.at, w)
		}
	}
}

func parse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func BenchmarkWindow(b *testing.B) {
	for _, cold := range []bool{false, true} {
		name := "warm"
		if cold {
			name = "cold"
		}
		b.Run(name, func(b *testing.B) {
			e, _ := Compile([]api.Schedule{{Name: "a", Cron: "* * * * *", Duration: metav1.Duration{Duration: time.Minute}}})
			now := parse("2026-10-01T10:00:00Z")
			e.At(now)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				at := now
				if cold {
					at = now.Add(time.Duration(i+1) * 2 * time.Hour)
				}
				if !e.At(at).Active {
					b.Fatal("inactive")
				}
			}
		})
	}
}
