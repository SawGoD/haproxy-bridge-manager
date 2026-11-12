package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var (
	ErrLocked               = errors.New("storage: target is locked")
	ErrHashConflict         = errors.New("storage: hash conflict")
	ErrExpectedHashRequired = errors.New("storage: expected hash required for existing file")
	ErrValidatorRequired    = errors.New("storage: candidate validator required")
	ErrBackupRequired       = errors.New("storage: backup required before mutation")
	ErrNoSpace              = errors.New("storage: insufficient free space")
	ErrUnsafeTarget         = errors.New("storage: target is not a regular file")
	ErrWriteVerification    = errors.New("storage: post-write verification failed")
)

type Validator interface {
	Validate(candidate []byte) error
}

type ValidatorFunc func([]byte) error

func (f ValidatorFunc) Validate(candidate []byte) error {
	return f(candidate)
}

type Backup interface {
	Backup(path string) error
}

type Owner struct {
	UID int
	GID int
}

type WriteOptions struct {
	ExpectedHash string
	Mode         os.FileMode
	Owner        *Owner
}

type Writer struct {
	Validator Validator
	Backup    Backup
	freeSpace func(string) (uint64, error)
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrUnsafeTarget, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%w: %s", ErrUnsafeTarget, path)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func FileHash(path string) (string, error) {
	data, err := readRegular(path)
	if err != nil {
		return "", err
	}
	return Hash(data), nil
}

func Read(path string) ([]byte, string, error) {
	data, err := readRegular(path)
	if err != nil {
		return nil, "", err
	}
	return data, Hash(data), nil
}

func (w *Writer) Write(path string, candidate []byte, expectedHash string) error {
	return w.WriteWithOptions(path, candidate, WriteOptions{ExpectedHash: expectedHash})
}

func (w *Writer) WriteWithOptions(path string, candidate []byte, options WriteOptions) (result error) {
	if w.Validator == nil {
		return ErrValidatorRequired
	}
	if err := w.Validator.Validate(candidate); err != nil {
		return fmt.Errorf("storage: candidate rejected: %w", err)
	}
	if path == "" || filepath.Base(path) != filepath.Base(filepath.Clean(path)) {
		return fmt.Errorf("storage: invalid target path %q", path)
	}
	lock, err := acquireLock(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := lock.Close(); result == nil && closeErr != nil {
			result = closeErr
		}
	}()

	info, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists && !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrUnsafeTarget, path)
	}
	if exists {
		if options.ExpectedHash == "" {
			return ErrExpectedHashRequired
		}
		actual, err := FileHash(path)
		if err != nil {
			return err
		}
		if actual != options.ExpectedHash {
			return fmt.Errorf("%w: expected %s, got %s", ErrHashConflict, options.ExpectedHash, actual)
		}
	} else if options.ExpectedHash != "" {
		return fmt.Errorf("%w: target does not exist", ErrHashConflict)
	}

	freeSpace := w.freeSpace
	if freeSpace == nil {
		freeSpace = availableSpace
	}
	free, err := freeSpace(filepath.Dir(path))
	if err != nil {
		return err
	}
	if free < uint64(len(candidate)) {
		return fmt.Errorf("%w: need %d bytes, have %d", ErrNoSpace, len(candidate), free)
	}
	if exists {
		if w.Backup == nil {
			return ErrBackupRequired
		}
		if err := w.Backup.Backup(path); err != nil {
			return fmt.Errorf("storage: backup failed: %w", err)
		}
	}

	mode := options.Mode
	if mode == 0 {
		mode = 0o600
		if exists {
			mode = info.Mode()
		}
	}
	owner := options.Owner
	if owner == nil && exists {
		if existingOwner, ok := fileOwner(info); ok {
			owner = &existingOwner
		}
	}
	beforeRename := func() error {
		current, err := os.Lstat(path)
		currentExists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if currentExists && !current.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", ErrUnsafeTarget, path)
		}
		if !currentExists {
			if options.ExpectedHash != "" {
				return fmt.Errorf("%w: target disappeared before rename", ErrHashConflict)
			}
			return nil
		}
		if options.ExpectedHash == "" {
			return fmt.Errorf("%w: target appeared before rename", ErrHashConflict)
		}
		actual, err := FileHash(path)
		if err != nil {
			return err
		}
		if actual != options.ExpectedHash {
			return fmt.Errorf("%w: target changed after backup: expected %s, got %s", ErrHashConflict, options.ExpectedHash, actual)
		}
		return nil
	}
	return writeTempRename(path, candidate, mode, owner, beforeRename)
}
