package panel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestPopupManagerContext(t *testing.T) {
	context := map[string]string{"HERDR_ENV": "1", "HERDR_PLUGIN_ID": "lanwen.herdr-sbx", "HERDR_PLUGIN_ENTRYPOINT_ID": "status", "HERDR_PANE_ID": ""}
	for name, value := range context {
		t.Setenv(name, value)
	}
	require.True(t, popupManager())
	for name, value := range map[string]string{"HERDR_ENV": "", "HERDR_PLUGIN_ID": "other.plugin", "HERDR_PLUGIN_ENTRYPOINT_ID": "other", "HERDR_PANE_ID": "pane-1"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			require.False(t, popupManager())
		})
	}
}

func TestPopupEscapeArmsConfirmation(t *testing.T) {
	for _, second := range []tea.KeyPressMsg{keyMsg(tea.KeyEscape, ""), keyMsg('q', "q"), {Code: 'c', Mod: tea.ModCtrl}} {
		t.Run(second.String(), func(t *testing.T) {
			m := modelFixture(t)
			m.popup = true
			before := time.Now()
			require.NotNil(t, m.key(keyMsg(tea.KeyEscape, "")))
			require.WithinDuration(t, before.Add(5*time.Second), m.quitUntil, time.Second)
			require.False(t, m.closing)
			command := m.key(second)
			require.NotNil(t, command)
			require.IsType(t, tea.QuitMsg{}, command())
			require.True(t, m.quitUntil.IsZero())
		})
	}
}

func TestPopupQuitResetAndTimeout(t *testing.T) {
	m := modelFixture(t)
	m.popup = true
	m.key(keyMsg(tea.KeyEscape, ""))
	old := m.quitUntil
	m.key(keyMsg('p', "p"))
	require.True(t, m.quitUntil.IsZero())
	require.Equal(t, "Plugins", m.page, "the reset key must still act normally")
	require.Nil(t, m.key(keyMsg(tea.KeyEscape, "")), "Escape first returns to Sandboxes")
	require.NotNil(t, m.key(keyMsg(tea.KeyEscape, "")))
	current := m.quitUntil
	m.Update(quitTimeoutMsg(old))
	require.Equal(t, current, m.quitUntil, "old timer cancelled a new attempt")
	m.Update(quitTimeoutMsg(current))
	require.True(t, m.quitUntil.IsZero())
	// A delayed timer must not extend the confirmation window.
	m.quitUntil = time.Now().Add(-time.Nanosecond)
	require.NotNil(t, m.key(keyMsg(tea.KeyEscape, "")))
	require.True(t, m.quitUntil.After(time.Now()))
	require.False(t, m.closing)
}

func TestPopupEscapeKeepsLocalActions(t *testing.T) {
	for _, context := range []string{"input", "dialog", "help", "controls", "tab", "filter", "drag"} {
		t.Run(context, func(t *testing.T) {
			m := modelFixture(t)
			m.popup = true
			switch context {
			case "input":
				m.input = &inputState{kind: "filter"}
			case "dialog":
				m.dialog = "startup"
			case "help":
				m.helpOpen = true
			case "controls":
				m.focus = "actions"
			case "tab":
				m.page = "Plugins"
			case "filter":
				m.queries[m.page] = "alpha"
			case "drag":
				m.textSelection = &textSelection{pressed: true}
			}
			require.Nil(t, m.key(keyMsg(tea.KeyEscape, "")))
			require.True(t, m.quitUntil.IsZero())
			require.Nil(t, m.input)
			require.Empty(t, m.dialog)
			require.False(t, m.helpOpen)
			require.Equal(t, "table", m.focus)
			require.Equal(t, "Sandboxes", m.page)
			require.Empty(t, m.queries[m.page])
		})
	}
}

func TestOrdinaryManagerQuitAndPopupPendingWork(t *testing.T) {
	m := modelFixture(t)
	m.popup = false
	require.Nil(t, m.key(keyMsg(tea.KeyEscape, "")))
	command := m.key(keyMsg('q', "q"))
	require.IsType(t, tea.QuitMsg{}, command())
	m.popup = true
	m.pending["a"] = true
	m.key(keyMsg(tea.KeyEscape, ""))
	require.False(t, m.closing)
	require.Nil(t, m.key(keyMsg(tea.KeyEscape, "")))
	require.True(t, m.closing)
	require.Contains(t, m.notice, "Finishing active work")
	_, command = m.Update(operationMsg{target: "a", code: store.CodeRename})
	require.NotNil(t, command)
	require.IsType(t, tea.QuitMsg{}, command())
	require.Empty(t, m.pending)
}

func TestCtrlCForcesCloseWhileOperationIsBlocked(t *testing.T) {
	for _, popup := range []bool{false, true} {
		t.Run(fmt.Sprintf("popup=%t", popup), func(t *testing.T) {
			m := modelFixture(t)
			m.popup = popup
			startBlockedQuitOperation(t, m)
			command := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			require.NotNil(t, command)
			require.IsType(t, tea.QuitMsg{}, command())
			require.True(t, m.pending["a"], "immediate close must not claim that work settled")
		})
	}
}

func TestCtrlCForcesCloseFromLocalInputWhileClosing(t *testing.T) {
	for _, local := range []string{"input", "dialog", "help"} {
		t.Run(local, func(t *testing.T) {
			m := modelFixture(t)
			m.closing = true
			switch local {
			case "input":
				m.input = &inputState{kind: "filter"}
			case "dialog":
				m.dialog = "startup"
			case "help":
				m.helpOpen = true
			}
			command := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			require.NotNil(t, command)
			require.IsType(t, tea.QuitMsg{}, command())
		})
	}
}

func startBlockedQuitOperation(t *testing.T, m *model) {
	t.Helper()
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	command := m.run("a", store.CodeRename, "Saved", func() error {
		close(started)
		<-release
		return nil
	})
	go func() {
		command()
		close(finished)
	}()
	<-started
	t.Cleanup(func() {
		close(release)
		<-finished
	})
}

func TestPopupQuitHintFrames(t *testing.T) {
	for _, size := range [][2]int{{110, 32}, {40, 12}} {
		m := modelFixture(t)
		m.popup = true
		m.width, m.height = size[0], size[1]
		m.notice = "Preparation in progress."
		m.key(keyMsg(tea.KeyEscape, ""))
		frame := ansi.Strip(m.View().Content)
		require.NotContains(t, frame, "Preparation in progress.")
		requireQuitHintBox(t, frame)
		t.Logf("%dx%d armed frame:\n%s", size[0], size[1], frame)
		m.Update(quitTimeoutMsg(m.quitUntil))
		frame = ansi.Strip(m.View().Content)
		require.NotContains(t, frame, "again to quit")
		require.Contains(t, frame, "Preparation in progress.")
		m.key(keyMsg(tea.KeyEscape, ""))
		m.key(keyMsg('x', "x"))
		frame = ansi.Strip(m.View().Content)
		require.NotContains(t, frame, "again to quit")
		require.Contains(t, frame, "Preparation in progress.")
	}
}

func TestPopupQuitCountdown(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	now := time.Now()
	m.quitUntil = now.Add(5 * time.Second)
	for _, width := range []int{110, 40, 30} {
		m.width = width
		for _, elapsed := range []time.Duration{0, time.Nanosecond, time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second, 5*time.Second - time.Nanosecond} {
			seconds := 5 - int(elapsed/time.Second)
			var rendered []string
			for _, line := range m.quitHintLines(now.Add(elapsed)) {
				rendered = append(rendered, line.text)
				require.LessOrEqual(t, ansi.StringWidth(line.text), width-4)
			}
			text := strings.Join(rendered, "\n")
			suffix := fmt.Sprintf("...%ds", seconds)
			require.Contains(t, text, ink(colorMuted, suffix), "countdown must use muted color")
			require.Contains(t, text, colorInfo+"Press esc again")
			require.Contains(t, strings.Join(strings.Fields(ansi.Strip(text)), " "), quitHint+" "+suffix)
			require.NotContains(t, text, "within")
		}
	}
}

func TestPopupQuitHintStaysVisibleWithLongContent(t *testing.T) {
	for _, size := range [][2]int{{30, 10}, {40, 12}} {
		for _, notice := range []string{"", strings.Repeat("A long operation notice. ", 20)} {
			m := modelFixture(t)
			m.popup = true
			m.width, m.height = size[0], size[1]
			m.notice = notice
			m.key(keyMsg(tea.KeyEscape, ""))
			frame := ansi.Strip(m.View().Content)
			requireQuitHintBox(t, frame)
			require.False(t, m.hintInBody)
			require.Equal(t, notice, m.notice)
			t.Logf("%dx%d, long notice %t:\n%s", size[0], size[1], notice != "", frame)
		}
	}
}

func requireQuitHintBox(t *testing.T, frame string) {
	t.Helper()
	require.Equal(t, 1, strings.Count(frame, "╭─ Hint"))
	_, box, found := strings.Cut(frame, "╭─ Hint")
	require.True(t, found)
	box, rest, found := strings.Cut(box, "╰")
	require.True(t, found, "Hint box must have its closing border")
	require.Contains(t, strings.Join(strings.Fields(strings.ReplaceAll(box, "│", " ")), " "), quitHint)
	require.Contains(t, box, "...5s")
	require.NotContains(t, rest, "Press esc", "quit prompt must stay inside Hint")
}

func TestQuitBindingsCloseImmediately(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{keyMsg('q', "q"), {Code: 'c', Mod: tea.ModCtrl}} {
		for _, popup := range []bool{false, true} {
			for _, pending := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/popup=%t/pending=%t", key.String(), popup, pending), func(t *testing.T) {
					m := modelFixture(t)
					m.popup = popup
					if pending {
						m.pending["a"] = true
					}
					command := m.key(key)
					require.NotNil(t, command, "quit bindings must not wait for pending work")
					require.IsType(t, tea.QuitMsg{}, command(), "quit bindings must not arm confirmation")
					require.True(t, m.quitUntil.IsZero())
				})
			}
		}
	}
}

func TestCtrlCQuitsImmediatelyFromLocalControls(t *testing.T) {
	for _, context := range []string{"input", "dialog", "help"} {
		t.Run(context, func(t *testing.T) {
			m := modelFixture(t)
			m.popup = true
			switch context {
			case "input":
				m.input = &inputState{kind: "filter", text: "unsaved"}
			case "dialog":
				m.dialog = "startup"
			case "help":
				m.helpOpen = true
			}
			command := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			require.NotNil(t, command)
			require.IsType(t, tea.QuitMsg{}, command())
		})
	}
}

func TestQuitLetterRemainsEditableText(t *testing.T) {
	m := modelFixture(t)
	m.popup = true
	m.input = &inputState{kind: "filter"}
	require.Nil(t, m.key(keyMsg('q', "q")))
	require.Equal(t, "q", m.input.text)
	require.True(t, m.quitUntil.IsZero())
}

func TestQuitLetterClosesConfirmation(t *testing.T) {
	m := modelFixture(t)
	m.popup, m.dialog = true, "startup"
	command := m.key(keyMsg('q', "q"))
	require.NotNil(t, command)
	require.IsType(t, tea.QuitMsg{}, command())
	require.True(t, m.quitUntil.IsZero())
}
