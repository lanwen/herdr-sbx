package panel

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestDetailsCachedServerVersionAndWidth(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		for _, width := range []int{36, 60, 110} {
			m := modelFixture(t)
			if noColor {
				t.Setenv("NO_COLOR", "1")
			} else {
				unsetNoColor(t)
			}
			m.width = width
			r := m.rows[0]
			now := time.Now().Add(-2 * time.Minute)
			r.Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2", ObservedAt: now, CheckedAt: now, Current: true}
			if got := guestServerDescription(r); got != "herdr 0.9.2" {
				t.Fatalf("current: %q", got)
			}
			lines := m.sandboxDetails(r)
			header := lines[0]
			if width >= 60 && !strings.HasSuffix(ansi.Strip(header.text), " herdr 0.9.2 ╮") {
				t.Fatalf("not in top-right badge: %q", header.text)
			}
			if !strings.Contains(ansi.Strip(header.text), "alpha") {
				t.Fatalf("version displaced sandbox name: %q", header.text)
			}
			for _, line := range lines {
				if ansi.StringWidth(line.text) > width-4 {
					t.Fatalf("overflow width %d: %q", width, line.text)
				}
				if noColor && strings.Contains(line.text, "\x1b[") {
					t.Fatal("NO_COLOR contains styling")
				}
			}
			for _, state := range []string{"failed probe", "stopped", "stale", "old"} {
				stale := r
				observed := *r.Status.GuestServer
				stale.Status.GuestServer = &observed
				switch state {
				case "failed probe":
					observed.Current = false
				case "stopped":
					stale.Sandbox.Status = "stopped"
				case "stale":
					stale.Status.Stale = true
				case "old":
					observed.CheckedAt = time.Now().Add(-16 * time.Minute)
				}
				if got := guestServerDescription(stale); got != "herdr 0.9.2" {
					t.Fatalf("%s: %q", state, got)
				}
			}
			r.Status.GuestServer = nil
			if guestServerDescription(r) != "" {
				t.Fatal("invented version")
			}
			withoutVersion := m.sandboxDetails(r)
			if strings.Contains(ansi.Strip(withoutVersion[0].text), "herdr") {
				t.Fatal("unknown version has a border label")
			}
			r.Status.GuestServer = &store.GuestServerObservation{}
			if guestServerDescription(r) != "" {
				t.Fatal("empty cached version displayed")
			}
		}
	}
}

func TestDetailsOverlongVersionKeepsBorderIntact(t *testing.T) {
	m := modelFixture(t)
	m.width = 36
	r := m.rows[0]
	r.Status.GuestServer = &store.GuestServerObservation{Version: strings.Repeat("1", 60)}
	lines := m.sandboxDetails(r)
	footer := ansi.Strip(lines[len(lines)-1].text)
	if ansi.StringWidth(footer) != m.width-4 || !strings.HasSuffix(footer, "─╯") || strings.Contains(footer, "herdr") {
		t.Fatalf("overlong version damaged footer: %q", footer)
	}
}

func TestDetailsObservationAgeStartsAfterTwoMinutes(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{0, ""},
		{time.Minute, ""},
		{2*time.Minute - time.Nanosecond, ""},
		{2 * time.Minute, ""},
		{2*time.Minute + time.Nanosecond, "2m ago"},
		{5 * time.Minute, "5m ago"},
	} {
		if got := detailsObservationAge(now.Add(-tc.age), now); got != tc.want {
			t.Fatalf("age %s: got %q, want %q", tc.age, got, tc.want)
		}
	}
	if detailsObservationAge(time.Time{}, now) != "" {
		t.Fatal("unknown observation age displayed")
	}
	// Keep relativeTime's other callers unchanged.
	if relativeTime(now) != "just now" {
		t.Fatal("shared relative time changed")
	}
	for _, width := range []int{36, 60, 110} {
		m := modelFixture(t)
		m.width = width
		r := m.rows[0]
		r.Status.ObservedAt = now
		header := ansi.Strip(m.sandboxDetails(r)[0].text)
		if strings.Contains(header, "just now") || !strings.Contains(header, "alpha (running)") {
			t.Fatalf("width %d: header %q", width, header)
		}
	}
}
