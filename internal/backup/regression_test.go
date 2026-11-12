package backup

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"testing"
	"time"
)

func TestBackupFilenameContentAndRetentionMinimum(t *testing.T) {
	if _, err := NewManager(t.TempDir(), 0); err == nil {
		t.Fatal("zero retention accepted")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(source, []byte("verified content"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(filepath.Join(dir, "backups"), 1)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return time.Date(2026, 11, 12, 13, 14, 15, 0, time.UTC) }
	metadata, err := manager.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^haproxy\.cfg\.2026-11-12_13-14-15-[0-9a-f]{12}\.bak$`)
	if !pattern.MatchString(metadata.ID) {
		t.Fatalf("unexpected backup filename %q", metadata.ID)
	}
	content, err := manager.Content(metadata.ID)
	if err != nil || string(content) != "verified content" {
		t.Fatalf("Content=%q err=%v", content, err)
	}
	if err := os.WriteFile(manager.dataPath(metadata.ID), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Content(metadata.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt content accepted: %v", err)
	}
}

func TestSetFlagsKeepsMetadataOwnershipAndMode(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(source, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager, _ := NewManager(filepath.Join(dir, "backups"), 1)
	metadata, err := manager.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(manager.metadataPath(metadata.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetFlags(metadata.ID, true, true); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(manager.metadataPath(metadata.ID))
	if err != nil {
		t.Fatal(err)
	}
	beforeOwner := before.Sys().(*syscall.Stat_t)
	afterOwner := after.Sys().(*syscall.Stat_t)
	if beforeOwner.Uid != afterOwner.Uid || beforeOwner.Gid != afterOwner.Gid || after.Mode().Perm() != 0o600 {
		t.Fatalf("metadata ownership/mode changed: before=%v after=%v", beforeOwner, afterOwner)
	}
}

func TestBackupRejectsSymlinkSource(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.cfg")
	if err := os.WriteFile(real, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "haproxy.cfg")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	manager, _ := NewManager(filepath.Join(dir, "backups"), 1)
	if _, err := manager.Create(link); err == nil {
		t.Fatal("symlink source accepted")
	}
}
