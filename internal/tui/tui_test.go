package tui

import (
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
	m.editor(true)
	if len(m.fields) != 7 || strings.Contains(m.nodeView(), "Название ноды") {
		t.Fatal("redundant name field remains")
	}
	for i, v := range []string{"nl1_in", "nl1_out", "127.0.0.1", "10447", "5s", "30s", "30s"} {
		m.fields[i].SetValue(v)
	}
	m.area.SetValue("public [2001:db8::1]:443 primary 1 check enabled 3s 2 2\nnb 100.100.10.20:443 backup 1 check enabled 3s 2 2")
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
