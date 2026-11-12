//go:build unix

package storage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func acquireLock(path string) (*os.File, error) {
	lockPath := path + ".lock"
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: unsafe lock file %s", ErrUnsafeTarget, lockPath)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := lock.Stat()
	if err != nil {
		lock.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		lock.Close()
		return nil, fmt.Errorf("%w: unsafe lock file %s", ErrUnsafeTarget, lockPath)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, path)
		}
		return nil, err
	}
	return lock, nil
}

func availableSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func fileOwner(info os.FileInfo) (Owner, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Owner{}, false
	}
	return Owner{UID: int(stat.Uid), GID: int(stat.Gid)}, true
}

func writeTempRename(path string, data []byte, mode os.FileMode, owner *Owner, beforeRename func() error) (result error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if closeErr := temp.Close(); result == nil && closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			result = closeErr
		}
		if removeErr := os.Remove(tempPath); result == nil && removeErr != nil && !os.IsNotExist(removeErr) {
			result = removeErr
		}
	}()
	if owner != nil {
		if err := os.Chown(tempPath, owner.UID, owner.GID); err != nil {
			return fmt.Errorf("storage: preserve ownership: %w", err)
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
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	written, err := readRegular(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWriteVerification, err)
	}
	if !bytes.Equal(written, data) {
		return fmt.Errorf("%w: content hash mismatch", ErrWriteVerification)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("storage: sync directory: %w", err)
	}
	return nil
}
