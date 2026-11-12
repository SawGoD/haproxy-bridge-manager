package storage

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

var errTestBackup = errors.New("no backup")

type recordingBackup struct {
	calls int
	err   error
}

func (b *recordingBackup) Backup(string) error {
	b.calls++
	return b.err
}

func acceptingWriter() (*Writer, *recordingBackup) {
	backup := &recordingBackup{}
	return &Writer{Validator: ValidatorFunc(func([]byte) error { return nil }), Backup: backup}, backup
}

func TestAtomicWriteLifecycleAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haproxy.cfg")
	writer, backup := acceptingWriter()
	if err := writer.Write(path, []byte("first"), ""); err != nil {
		t.Fatal(err)
	}
	if backup.calls != 0 {
		t.Fatal("new file was backed up")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	beforeOwner := before.Sys().(*syscall.Stat_t)
	_, hash, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(path, []byte("second"), hash); err != nil {
		t.Fatal(err)
	}
	data, newHash, _ := Read(path)
	if string(data) != "second" || newHash == hash || backup.calls != 1 {
		t.Fatalf("data=%q backup=%d", data, backup.calls)
	}
	after, _ := os.Stat(path)
	afterOwner := after.Sys().(*syscall.Stat_t)
	if after.Mode().Perm() != 0o640 || afterOwner.Uid != beforeOwner.Uid || afterOwner.Gid != beforeOwner.Gid {
		t.Fatalf("metadata not preserved: %v %v", after.Mode(), afterOwner)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".haproxy.cfg.tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v %v", matches, err)
	}
	if info, err := os.Stat(path + ".lock"); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("persistent lock file missing or invalid: %v %v", info, err)
	}
}

func TestWriteSafetyFailuresLeaveTargetUntouched(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(*Writer, *recordingBackup)
		expected string
		want     error
		backups  int
	}{
		{name: "missing validator", want: ErrValidatorRequired},
		{name: "missing expected hash", want: ErrExpectedHashRequired},
		{name: "hash conflict", expected: "wrong", want: ErrHashConflict},
		{name: "backup required", expected: "present", prepare: func(w *Writer, _ *recordingBackup) { w.Backup = nil }, want: ErrBackupRequired},
		{name: "backup failure", expected: "present", prepare: func(_ *Writer, b *recordingBackup) { b.err = errTestBackup }, want: errTestBackup, backups: 1},
		{name: "no space", expected: "present", prepare: func(w *Writer, _ *recordingBackup) { w.freeSpace = func(string) (uint64, error) { return 1, nil } }, want: ErrNoSpace},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.cfg")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			writer, backup := acceptingWriter()
			if test.name == "missing validator" {
				writer.Validator = nil
			}
			if test.prepare != nil {
				test.prepare(writer, backup)
			}
			expected := test.expected
			if expected == "present" {
				expected = Hash([]byte("original"))
			}
			err := writer.Write(path, []byte("replacement"), expected)
			if !errors.Is(err, test.want) {
				t.Fatalf("want %v, got %v", test.want, err)
			}
			data, _ := os.ReadFile(path)
			if string(data) != "original" || backup.calls != test.backups {
				t.Fatalf("data=%q backups=%d", data, backup.calls)
			}
		})
	}
}

func TestInvalidCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.cfg")
	writer, backup := acceptingWriter()
	writer.Validator = ValidatorFunc(func([]byte) error { return errors.New("bad config") })
	if err := writer.Write(path, []byte("bad"), ""); err == nil || backup.calls != 0 {
		t.Fatalf("err=%v backups=%d", err, backup.calls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid candidate created target")
	}
}

func TestExistingTargetRequiresHashForNewValidation(t *testing.T) {
	writer, _ := acceptingWriter()
	path := filepath.Join(t.TempDir(), "config.cfg")
	if err := writer.Write(path, []byte("new"), ""); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(path, []byte("changed"), Hash([]byte("new"))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(path, []byte("changed again"), Hash([]byte("new"))); !errors.Is(err, ErrHashConflict) {
		t.Fatalf("got %v", err)
	}
}
