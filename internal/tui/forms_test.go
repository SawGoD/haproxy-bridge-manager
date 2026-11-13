package tui

import (
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	cfg := settings.Default()
	cfg.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	cfg.General.BackupDir = filepath.Join(dir, "backups")
	b, e := os.ReadFile("../../testdata/haproxy.cfg")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cfg.General.ConfigPath, b, 0600); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "config.toml")
	if e = cfg.Save(p); e != nil {
		t.Fatal(e)
	}
	return New(p)
}
func press(m Model, key tea.KeyMsg) Model { n, _ := m.Update(key); return n.(Model) }
func TestStructuredSettingsAndCancel(t *testing.T) {
	m := sampleModel(t)
	original := m.cfg.General.ConfigPath
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if len(m.prefs) == 0 || strings.Contains(m.View(), "config_path =") {
		t.Fatal("raw TOML shown")
	}
	m.prefs[0].input.SetValue("/tmp/changed.cfg")
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.cfg.General.ConfigPath != original || m.mode != "main" {
		t.Fatal("cancel changed settings")
	}
}
func TestServerFormAndConfirmedDelete(t *testing.T) {
	m := sampleModel(t)
	m.editor(false)
	before := m.area.Value()
	m = press(m, tea.KeyMsg{Type: tea.KeyF2})
	if m.mode != "servers" {
		t.Fatal(m.mode)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.mode != "serverform" {
		t.Fatal(m.mode)
	}
	m.serverFields[0].SetValue("public6")
	m.serverFields[1].SetValue("[2001:db8::1]:443")
	m.serverFields[2].SetValue("backup")
	if e := m.storeServer(); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(m.area.Value(), "public6 [2001:db8::1]:443 backup") {
		t.Fatal(m.area.Value())
	}
	m.serverIndex = 2
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.mode != "confirm" || len(m.serverLines()) != 3 {
		t.Fatal("deleted without confirmation")
	}
	msg := m.action()
	n, _ := m.Update(msg)
	m = n.(Model)
	if m.mode != "servers" || m.area.Value() != before {
		t.Fatal("confirmed deletion not applied to editor", m.mode, m.area.Value())
	}
}
func TestLayoutSizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 36}} {
		m := sampleModel(t)
		n, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = n.(Model)
		for _, mode := range []string{"main", "editor", "servers", "serverform", "settings", "wizard"} {
			switch mode {
			case "editor":
				m.editor(false)
			case "servers":
				m.mode = "servers"
			case "serverform":
				m.serverIndex = 0
				m.openServer(false)
			case "settings":
				m.beginPreferences(false)
			case "wizard":
				m.beginPreferences(true)
			}
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%s exceeds height: %d>%d", mode, len(lines), size[1])
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%s exceeds width: %d>%d", mode, ansi.StringWidth(line), size[0])
				}
			}
			if mode == "main" && !strings.Contains(view, "UNKNOWN") {
				t.Fatal("status hidden")
			}
		}
	}
}
func TestDiffShowsOnlyChangeContext(t *testing.T) {
	old := strings.Repeat("unchanged\n", 20) + "old\n" + strings.Repeat("tail\n", 20)
	next := strings.Replace(old, "old\n", "new\n", 1)
	got := diff([]byte(old), []byte(next))
	if !strings.Contains(got, "-old\n+new") || strings.Count(got, "unchanged") > 3 {
		t.Fatal(got)
	}
}
