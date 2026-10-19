package panel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestSandboxColumnsCompactManagementBeforeSandboxName(t *testing.T) {
	for _, test := range []struct {
		name       string
		width      int
		nameWidth  int
		labelWidth int
		truncated  bool
	}{
		{"without readiness", 64, 37, 9, false},
		{"with readiness", 80, 40, 9, false},
		{"long label without readiness", 64, 37, 80, false},
		{"long label with readiness", 80, 40, 80, false},
		{"still too long", 80, 80, 80, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := modelFixture(t)
			m.width = test.width
			m.rows = m.rows[:1]
			m.rows[0].Sandbox.Name = strings.Repeat("s", test.nameWidth)
			m.rows[0].Label = strings.Repeat("l", test.labelWidth)
			m.rows[0].Status.Connection = "ready"
			columns := m.sandboxColumns(m.rows)
			if !columns.compactManagement || columns.managementText(store.Unmanaged) != "UM" {
				t.Fatalf("management was not compacted first: %+v", columns)
			}
			if got := columns.name < test.nameWidth; got != test.truncated {
				t.Fatalf("name width=%d, want truncated=%v for %d cells: %+v", columns.name, test.truncated, test.nameWidth, columns)
			}
			row := columns.render("    ", "UM", m.rows[0].Sandbox.Name, "●", m.rows[0].Label, "ready")
			if ansi.StringWidth(row) != test.width-4 || columns.label < 1 || columns.name < 1 {
				t.Fatalf("invalid fitted row: %+v: %q", columns, row)
			}
		})
	}
}

func TestSandboxColumnsAdaptToContent(t *testing.T) {
	for _, test := range []struct {
		name          string
		width         int
		long, compact bool
	}{{"roomy", 110, false, false}, {"long at110", 110, true, true}, {"narrow", 60, false, false}, {"long at60", 60, true, true}} {
		t.Run(test.name, func(t *testing.T) {
			m := modelFixture(t)
			m.width = test.width
			m.height = 18
			m.rows[0].Status.Connection = "ready"
			if test.long {
				m.rows[0].Sandbox.Name = strings.Repeat("sandbox-", 6)
				m.rows[0].Label = strings.Repeat("Herdr label ", 5)
			}
			columns := m.sandboxColumns(m.rows)
			if columns.compactRuntime != test.compact {
				t.Fatalf("compact=%v, want %v: %+v", columns.compactRuntime, test.compact, columns)
			}
			header := columns.header()
			if ansi.StringWidth(header) != test.width-4 {
				t.Fatalf("table width=%d, want %d", ansi.StringWidth(header), test.width-4)
			}
			lines := m.sandboxLines()
			row := ansi.Strip(lines[3].text)
			readinessX := 4 + columns.management + 1 + columns.name + 1 + columns.runtime + 1 + columns.label + 1
			if columns.readiness > 0 && (strings.TrimSpace(ansi.Cut(header, readinessX, test.width-4)) != "READINESS" || strings.TrimSpace(ansi.Cut(row, readinessX, test.width-4)) != "ready") {
				t.Fatalf("readiness not aligned: %q\n%q", header, row)
			}
			m.beginRename()
			m.input.text = strings.Repeat("label ", 30)
			m.input.cursor = len([]rune(m.input.text))
			m.reveal = true
			columns = m.sandboxColumns(m.rows)
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "label │") {
				t.Fatalf("inline editor cursor clipped with label width %d: %s", columns.label, view)
			}
			for _, h := range m.hits {
				if h.action == "expand" && h.id == "a" {
					rendered := strings.Split(view, "\n")[h.y]
					if ansi.Cut(rendered, h.x, h.x+h.w) != "▸ " {
						t.Fatal("disclosure geometry changed")
					}
				}
			}
		})
	}
	t.Run("runtime details and no color symbols", func(t *testing.T) {
		m := modelFixture(t)
		m.rows[0].Status.Connection = "ready"
		m.rows[0].Status.Stale = true
		m.expanded = "a"
		columns := m.sandboxColumns(m.rows)
		row := ansi.Strip(m.sandboxLines()[3].text)
		readinessX := 4 + columns.management + 1 + columns.name + 1 + columns.runtime + 1 + columns.label + 1
		if strings.TrimSpace(ansi.Cut(row, readinessX, m.width-4)) != "" {
			t.Fatal("expanded row still repeats readiness")
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "running (stale)") {
			t.Fatal("expanded runtime lost stale state")
		}
		t.Setenv("NO_COLOR", "")
		for state, want := range map[string]string{"running": "●", "stopped": "○", "unknown": "?"} {
			if runtimeDot(state) != want || ansi.StringWidth(runtimeDot(state)) != 1 {
				t.Fatalf("compact %s=%q", state, runtimeDot(state))
			}
		}
		unsetNoColor(t)
		if !strings.Contains(readinessStyle("ownership-conflict", "ownership-conflict"), colorWarning) {
			t.Fatal("ownership conflict is not amber")
		}
	})
}
