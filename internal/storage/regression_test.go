//go:build unix

package storage

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestStaleLockFileRecoversAndActiveFlockConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haproxy.cfg")
	if err := os.WriteFile(path+".lock", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, _ := acceptingWriter()
	if err := writer.Write(path, []byte("first"), ""); err != nil {
		t.Fatalf("stale lock was not recovered: %v", err)
	}
	lock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(path, []byte("second"), Hash([]byte("first"))); !errors.Is(err, ErrLocked) {
		t.Fatalf("active flock not honored: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(path, []byte("second"), Hash([]byte("first"))); err != nil {
		t.Fatalf("released flock was not recovered: %v", err)
	}
}

type mutatingBackup struct {
	path string
	data []byte
}

func (b mutatingBackup) Backup(string) error {
	return os.WriteFile(b.path, b.data, 0o600)
}

func TestHashRecheckedAfterBackupImmediatelyBeforeRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haproxy.cfg")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := Writer{Validator: ValidatorFunc(func([]byte) error { return nil }), Backup: mutatingBackup{path: path, data: []byte("concurrent")}}
	err := writer.Write(path, []byte("candidate"), Hash([]byte("original")))
	if !errors.Is(err, ErrHashConflict) {
		t.Fatalf("post-backup hash change accepted: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "concurrent" {
		t.Fatalf("candidate renamed over concurrent data: %q", data)
	}
}

func TestWriterRejectsSymlinksAndNonRegularTargets(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.cfg")
	if err := os.WriteFile(real, []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.cfg")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	writer, backup := acceptingWriter()
	if err := writer.Write(link, []byte("candidate"), Hash([]byte("real"))); !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("symlink accepted: %v", err)
	}
	if err := writer.Write(dir, []byte("candidate"), "anything"); !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("directory accepted: %v", err)
	}
	data, _ := os.ReadFile(real)
	if string(data) != "real" || backup.calls != 0 {
		t.Fatalf("unsafe target changed: %q backups=%d", data, backup.calls)
	}
}

func TestExternalFlockIsHonored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haproxy.cfg")
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	writer, _ := acceptingWriter()
	if err := writer.Write(path, []byte("data"), ""); !errors.Is(err, ErrLocked) {
		t.Fatalf("external flock ignored: %v", err)
	}
}
