//go:build unix

package backup

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/SawGoD/haproxy-bridge/internal/storage"
)

func ownerOf(info os.FileInfo) (storage.Owner, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return storage.Owner{}, false
	}
	return storage.Owner{UID: int(stat.Uid), GID: int(stat.Gid)}, true
}

func writeFile(path string, data []byte, mode os.FileMode, owner *storage.Owner) (result error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".backup-tmp-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		if result == nil {
			return
		}
		_ = os.Remove(tempPath)
	}()
	if owner != nil {
		if err := temp.Chown(owner.UID, owner.GID); err != nil {
			return err
		}
	}
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
