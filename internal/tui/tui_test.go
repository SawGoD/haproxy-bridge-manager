package tui

import (
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWizard(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "missing.toml"))
	if m.mode != "wizard" || !strings.Contains(m.View(), "Первый запуск") {
		t.Fatal(m.mode)
	}
}
func TestUnknownAndDeleteConfirmation(t *testing.T) {
	dir := t.TempDir()
	s := settings.Default()
	s.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	s.General.BackupDir = filepath.Join(dir, "backups")
	cfg := []byte("defaults\n mode tcp\n timeout connect 1s\n timeout client 1s\n timeout server 1s\nfrontend f\n bind 127.0.0.1:10443\n default_backend b\nbackend b\n server s 127.0.0.1:443 check\n")
	os.WriteFile(s.General.ConfigPath, cfg, 0600)
	p := filepath.Join(dir, "settings.toml")
	if e := s.Save(p); e != nil {
		t.Fatal(e)
	}
	m := New(p)
	if !strings.Contains(m.View(), "UNKNOWN") {
		t.Fatal(m.View())
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)
	if m.mode != "confirm" {
		t.Fatal(m.mode, m.notice)
	}
	if string(m.s.Document.Bytes()) != string(cfg) {
		t.Fatal("delete executed before confirmation")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).mode != "main" {
		t.Fatal("cancel failed")
	}
}

func TestFifthNodeEditor(t *testing.T) {
	dir := t.TempDir()
	cfg := settings.Default()
	cfg.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	cfg.General.BackupDir = filepath.Join(dir, "backups")
	b, e := os.ReadFile("../../testdata/haproxy.cfg")
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(cfg.General.ConfigPath, b, 0600)
	p := filepath.Join(dir, "config.toml")
	cfg.Save(p)
	m := New(p)
	if len(m.rows()) != 4 {
		t.Fatalf("expected four routes: %s", m.View())
	}
	m.openRoute(newRoute)
	if len(m.fields) != 7 || strings.Contains(m.routeEditorView(), "Название ноды") {
		t.Fatal("redundant name field remains")
	}
	for i, v := range []string{"nl1_in", "nl1_out", "127.0.0.1", "10447", "5s", "30s", "30s"} {
		m.fields[i].SetValue(v)
	}
	m.servers = []haproxy.Server{
		{Name: "public", Address: "2001:db8::1", Port: 443, Weight: 1, HealthCheck: true},
		{Name: "nb", Address: "100.100.10.20", Port: 443, Backup: true, Weight: 1, HealthCheck: true},
	}
	if e = m.candidate(); e != nil {
		t.Fatal(e)
	}
	if m.mode != "confirm" {
		t.Fatal(m.mode)
	}
	msg := m.action()
	if x := msg.(result); x.err != nil {
		t.Fatal(x.err)
	}
	if len(m.rows()) != 5 {
		t.Fatal("fifth route absent")
	}
	if !strings.Contains(string(m.s.Document.Bytes()), "lv1_in") {
		t.Fatal("lv1 renamed")
	}
	f, err := m.s.Document.Frontend("nl1_in")
	if err != nil || f.Bind.Port != 10447 || f.Bind.Address != "127.0.0.1" || f.Backend != "nl1_out" {
		t.Fatal(f, err)
	}
	backend, e := m.s.Document.Backend("nl1_out")
	if e != nil || len(backend.Servers) != 2 || !backend.Servers[1].Backup {
		t.Fatal(backend, e)
	}
}

func TestCopyRouteOffersFreePortAndKeepsIndependentBackend(t *testing.T) {
	m := sampleModel(t)
	withListener := append(m.s.Document.Bytes(), []byte("\nlisten reserved\n\tbind 127.0.0.2:10447\n")...)
	var err error
	m.s.Document, err = haproxy.Parse(withListener)
	if err != nil {
		t.Fatal(err)
	}
	runeKey := func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
	m = press(m, tea.KeyMsg{Type: tea.KeyTab})
	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.copySource != "de1_in" || m.fields[routePortField].Value() != "10448" || len(m.servers) != 2 {
		t.Fatal(m.View())
	}
	m.fields[routePortField].SetValue("10447")
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != "editor" || m.focus != routePortField || !strings.Contains(m.notice, "F3 — выбрать 10448") {
		t.Fatal(m.mode, m.notice)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyF3})
	m = press(m, tea.KeyMsg{Type: tea.KeyF2})
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.serverFields[1].SetValue("2001:db8::2")
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != "confirm" || string(m.s.Document.Bytes()) != string(withListener) {
		t.Fatal(m.mode, m.notice)
	}
	next, complete := m.Update(runeKey('y'))
	next, _ = next.Update(complete())
	m = next.(Model)
	f, err := m.s.Document.Frontend("de1_in_copy")
	if err != nil || f.Bind.Port != 10448 || f.Backend != "de1_out_copy" {
		t.Fatal(f, err)
	}
	original, err := m.s.Document.Server("de1_out", "de1-public")
	if err != nil || original.Address != "212.113.112.170" {
		t.Fatal(original, err)
	}
	updated, err := m.s.Document.Server("de1_out_copy", "de1-public")
	if err != nil || updated.Address != "2001:db8::2" {
		t.Fatal(updated, err)
	}
	contents := string(m.s.Document.Bytes())
	if strings.Count(contents, "option tcplog") != 2 || strings.Count(contents, "maxconn 32") != 4 {
		t.Fatal(contents)
	}
	m = press(m, runeKey('c'))
	if m.fields[0].Value() != "de1_in_copy2" || m.fields[routePortField].Value() != "10449" {
		t.Fatal(m.View())
	}
}
