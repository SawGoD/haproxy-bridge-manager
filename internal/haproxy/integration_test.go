package haproxy

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/SawGoD/haproxy-bridge/internal/backup"
	"github.com/SawGoD/haproxy-bridge/internal/storage"
)

func TestFixtureAtomicMutationBackupAndRestore(t *testing.T) {
	original := fixture(t)
	target := filepath.Join(t.TempDir(), "haproxy.cfg")
	if err := os.WriteFile(target, original, 0o640); err != nil {
		t.Fatal(err)
	}
	manager, err := backup.NewManager(filepath.Join(filepath.Dir(target), "backups"), 3)
	if err != nil {
		t.Fatal(err)
	}
	writer := storage.Writer{Validator: Validator{}, Backup: manager}
	document, _ := Parse(original)
	if err := document.UpdateFrontend("de1_in", Frontend{Name: "de1_renamed", Backend: "de1_out", Bind: Bind{Address: "127.0.0.1", Port: 11443}}); err != nil {
		t.Fatal(err)
	}
	if _, err := document.Frontend("de1_renamed"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(target, document.Bytes(), storage.Hash(original)); err != nil {
		t.Fatal(err)
	}
	mutated, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	check, _ := Parse(mutated)
	if _, err := check.Backend("lv1_out"); err != nil {
		t.Fatal("lv1 was not retained")
	}
	backups, err := manager.List()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%+v err=%v", backups, err)
	}
	if err := manager.Restore(backups[0].ID, target, storage.Hash(mutated), Validator{}); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatal("restore did not reproduce original fixture")
	}
}
