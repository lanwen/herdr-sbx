package panel

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
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
			{
				got := guestServerDescription(r)
				require.Equal(t, "herdr 0.9.2", got, "current: %q", got)
			}
			lines := m.sandboxDetails(r)
			header := lines[0]
			require.False(t, width >= 60 && !strings.HasSuffix(ansi.Strip(header.text), " herdr 0.9.2 ╮"), "not in top-right badge: %q", header.text)
			require.Contains(t, ansi.Strip(header.text), "alpha", "version displaced sandbox name: %q", header.text)
			for _, line := range lines {
				require.False(t, ansi.StringWidth(line.text) > width-4, "overflow width %d: %q", width, line.text)
				require.False(t, noColor && strings.Contains(line.text, "\x1b["), "NO_COLOR contains styling")
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
				{
					got := guestServerDescription(stale)
					require.Equal(t, "herdr 0.9.2", got, "%s: %q", state, got)
				}
			}
			r.Status.GuestServer = nil
			require.Equal(t, "", guestServerDescription(r), "invented version")
			withoutVersion := m.sandboxDetails(r)
			require.NotContains(t, ansi.Strip(withoutVersion[0].text), "herdr", "unknown version has a border label")
			r.Status.GuestServer = &store.GuestServerObservation{}
			require.Equal(t, "", guestServerDescription(r), "empty cached version displayed")
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
	require.False(t, ansi.StringWidth(footer) != m.width-4)
	require.True(t, strings.HasSuffix(footer, "─╯"))
	require.NotContains(t, footer, "herdr")
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
		got := detailsObservationAge(now.Add(-tc.age), now)
		require.Equal(t, tc.want, got, "age %s: got %q, want %q", tc.age, got, tc.want)
	}
	require.Equal(t, "", detailsObservationAge(time.Time{}, now), "unknown observation age displayed")
	// Keep relativeTime's other callers unchanged.
	require.Equal(t, "just now", relativeTime(now), "shared relative time changed")
	for _, width := range []int{36, 60, 110} {
		m := modelFixture(t)
		m.width = width
		r := m.rows[0]
		r.Status.ObservedAt = now
		header := ansi.Strip(m.sandboxDetails(r)[0].text)
		require.NotContains(t, header, "just now")
		require.Contains(t, header, "alpha (running)")
	}
}
