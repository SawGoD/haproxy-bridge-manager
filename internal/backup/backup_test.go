package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SawGoD/haproxy-bridge/internal/storage"
)

var acceptAll = storage.ValidatorFunc(func([]byte) error { return nil })

func TestBackupMetadataUniqueAndVerified(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(source, []byte("configuration"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(filepath.Join(dir, "backups"), 10)
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.SHA256 != storage.Hash([]byte("configuration")) || first.Mode.Perm() != 0o640 {
		t.Fatalf("bad metadata: %+v %+v", first, second)
	}
	info, err := os.Stat(manager.dataPath(first.ID))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("backup mode: %v %v", info, err)
	}
	if err := manager.Verify(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.dataPath(first.ID), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := manager.Verify(first.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
}

func TestRetentionPreservesPinnedAndGood(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.cfg")
	manager, err := NewManager(filepath.Join(dir, "backups"), 1)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	manager.now = func() time.Time { base = base.Add(time.Second); return base }
	create := func(content string) Metadata {
		t.Helper()
		if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		metadata, err := manager.Create(source)
		if err != nil {
			t.Fatal(err)
		}
		return metadata
	}
	pinned := create("pinned")
	if err := manager.SetFlags(pinned.ID, true, false); err != nil {
		t.Fatal(err)
	}
	good := create("good")
	if err := manager.SetFlags(good.ID, false, true); err != nil {
		t.Fatal(err)
	}
	_ = create("old ordinary")
	newest := create("newest ordinary")
	backups, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("got %d backups: %+v", len(backups), backups)
	}
	for _, id := range []string{pinned.ID, good.ID, newest.ID} {
		if err := manager.Verify(id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestRestoreValidatedAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(target, []byte("good"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(filepath.Join(dir, "backups"), 10)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	writer := storage.Writer{Validator: acceptAll, Backup: manager}
	goodHash := storage.Hash([]byte("good"))
	if err := writer.Write(target, []byte("current"), goodHash); err != nil {
		t.Fatal(err)
	}
	if err := manager.Restore(metadata.ID, target, storage.Hash([]byte("current")), acceptAll); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	info, _ := os.Stat(target)
	if string(data) != "good" || info.Mode().Perm() != 0o640 {
		t.Fatalf("data=%q mode=%v", data, info.Mode())
	}
	if err := manager.Restore(metadata.ID, target, storage.Hash([]byte("good")), storage.ValidatorFunc(func([]byte) error { return errors.New("reject") })); err == nil {
		t.Fatal("invalid restore accepted")
	}
	data, _ = os.ReadFile(target)
	if string(data) != "good" {
		t.Fatal("failed restore changed target")
	}
}

func TestRestoreCorruptionAndNewTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.cfg")
	if err := os.WriteFile(source, []byte("source"), 0o604); err != nil {
		t.Fatal(err)
	}
	manager, _ := NewManager(filepath.Join(dir, "backups"), 2)
	metadata, _ := manager.Create(source)
	newTarget := filepath.Join(dir, "new.cfg")
	if err := manager.Restore(metadata.ID, newTarget, "", acceptAll); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(newTarget)
	if info.Mode().Perm() != 0o604 {
		t.Fatalf("got mode %v", info.Mode())
	}
	if err := os.WriteFile(manager.dataPath(metadata.ID), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Restore(metadata.ID, newTarget, storage.Hash([]byte("source")), acceptAll); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
	if _, err := manager.readMetadata("../secret"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
