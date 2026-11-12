package app

import (
	"context"
	"errors"
	"github.com/SawGoD/haproxy-bridge/internal/haproxy"
	"github.com/SawGoD/haproxy-bridge/internal/settings"
	"github.com/SawGoD/haproxy-bridge/internal/storage"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type reject struct{}

func (reject) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("official HAProxy validator rejected candidate")
}
func TestOfficialRejectAndConflict(t *testing.T) {
	dir := t.TempDir()
	cfg := settings.Default()
	cfg.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	cfg.General.BackupDir = filepath.Join(dir, "backups")
	cfg.HAProxy.Mode = "native"
	data := []byte("global\n maxconn 100\ndefaults\n mode tcp\n")
	os.WriteFile(cfg.General.ConfigPath, data, 0600)
	s, e := Open(cfg)
	if e != nil {
		t.Fatal(e)
	}
	s.Runner = reject{}
	if s.Save() == nil {
		t.Fatal("invalid candidate saved")
	}
	b, _ := os.ReadFile(s.Path)
	if string(b) != string(data) {
		t.Fatal("invalid candidate replaced config")
	}
	os.WriteFile(s.Path, []byte("# external\n"), 0600)
	if e = s.Apply(); !errors.Is(e, storage.ErrHashConflict) {
		t.Fatalf("conflict not detected: %v", e)
	}
}

type nativeMock struct {
	failRestart bool
	configPath  string
}

func (r *nativeMock) Run(_ context.Context, name string, a ...string) ([]byte, error) {
	if name == "systemctl" && len(a) > 0 && a[0] == "show" {
		return []byte("{ path=/usr/sbin/haproxy ; argv[]=/usr/sbin/haproxy -Ws -f " + r.configPath + " ; }"), nil
	}
	if name == "systemctl" && len(a) > 0 && a[0] == "restart" && r.failRestart {
		r.failRestart = false
		return nil, errors.New("restart failed")
	}
	return nil, nil
}
func TestRollbackOnlyAfterConfirmedApply(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	dir := t.TempDir()
	cfg := settings.Default()
	cfg.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	cfg.General.BackupDir = filepath.Join(dir, "backups")
	cfg.HAProxy.Mode = "native"
	data := []byte("global\n maxconn 100\ndefaults\n mode tcp\nfrontend f\n bind " + l.Addr().String() + "\n default_backend b\nbackend b\n server s 127.0.0.1:443 check\n")
	os.WriteFile(cfg.General.ConfigPath, data, 0600)
	s, e := Open(cfg)
	if e != nil {
		t.Fatal(e)
	}
	runner := &nativeMock{configPath: cfg.General.ConfigPath}
	s.Runner = runner
	if e = s.Apply(); e != nil {
		t.Fatal(e)
	}
	d, e := haproxy.Parse(append(append([]byte{}, data...), []byte("# next revision\n")...))
	if e != nil {
		t.Fatal(e)
	}
	s.Document = d
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	runner.failRestart = true
	if e = s.Apply(); e == nil || !strings.Contains(e.Error(), "previous verified configuration restored") {
		t.Fatalf("rollback: %v", e)
	}
	got, _ := os.ReadFile(s.Path)
	if string(got) != string(data) {
		t.Fatal("rollback did not restore confirmed bytes")
	}
}
func TestDisplayDoesNotRequireBackupWriteAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := settings.Default()
	cfg.General.ConfigPath = filepath.Join(dir, "haproxy.cfg")
	os.WriteFile(cfg.General.ConfigPath, []byte("global\n"), 0600)
	block := filepath.Join(dir, "not-directory")
	os.WriteFile(block, []byte("x"), 0600)
	cfg.General.BackupDir = filepath.Join(block, "backups")
	s, e := Open(cfg)
	if e != nil {
		t.Fatalf("read-only open failed: %v", e)
	}
	if _, e = s.Backups.Create(s.Path); e == nil {
		t.Fatal("backup unexpectedly writable")
	}
}
