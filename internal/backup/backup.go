package backup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/SawGoD/haproxy-bridge/internal/storage"
)

var (
	ErrNotFound = errors.New("backup: not found")
	ErrCorrupt  = errors.New("backup: checksum mismatch")
)

type Metadata struct {
	ID        string        `json:"id"`
	Source    string        `json:"source"`
	SHA256    string        `json:"sha256"`
	Size      int64         `json:"size"`
	CreatedAt time.Time     `json:"created_at"`
	Mode      os.FileMode   `json:"mode"`
	Owner     storage.Owner `json:"owner"`
	Pinned    bool          `json:"pinned"`
	Good      bool          `json:"good"`
}

type Manager struct {
	dir       string
	retention int
	now       func() time.Time
}

func NewLazyManager(directory string, retention int) (*Manager, error) {
	if directory == "" || retention < 1 {
		return nil, fmt.Errorf("backup: invalid directory or retention")
	}
	return &Manager{dir: directory, retention: retention, now: time.Now}, nil
}
func NewManager(directory string, retention int) (*Manager, error) {
	m, e := NewLazyManager(directory, retention)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(directory, 0700); e != nil {
		return nil, e
	}
	return m, nil
}

func (m *Manager) Backup(path string) error {
	_, err := m.Create(path)
	return err
}

func (m *Manager) Create(path string) (Metadata, error) {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return Metadata{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, fmt.Errorf("backup: source is not a regular file: %s", path)
	}
	data, hash, err := storage.Read(path)
	if err != nil {
		return Metadata{}, err
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return Metadata{}, err
	}
	createdAt := m.now().UTC()
	id := filepath.Base(path) + "." + createdAt.Format("2006-01-02_15-04-05") + "-" + hex.EncodeToString(random) + ".bak"
	owner, _ := ownerOf(info)
	metadata := Metadata{
		ID:        id,
		Source:    path,
		SHA256:    hash,
		Size:      int64(len(data)),
		CreatedAt: createdAt,
		Mode:      info.Mode(),
		Owner:     owner,
	}
	if err := writeFile(m.dataPath(id), data, info.Mode(), nil); err != nil {
		return Metadata{}, err
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return Metadata{}, err
	}
	if err := writeFile(m.metadataPath(id), metadataBytes, 0o600, nil); err != nil {
		_ = os.Remove(m.dataPath(id))
		return Metadata{}, err
	}
	if err := m.Prune(); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func (m *Manager) List() ([]Metadata, error) {
	entries, err := os.ReadDir(m.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Metadata
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		metadata, err := m.readMetadata(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		result = append(result, metadata)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (m *Manager) SetFlags(id string, pinned, good bool) error {
	metadata, err := m.readMetadata(id)
	if err != nil {
		return err
	}
	metadata.Pinned = pinned
	metadata.Good = good
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return writeFile(m.metadataPath(id), data, 0o600, nil)
}

func (m *Manager) Content(id string) ([]byte, error) {
	metadata, data, err := m.read(id)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != metadata.Size || storage.Hash(data) != metadata.SHA256 {
		return nil, fmt.Errorf("%w: %s", ErrCorrupt, id)
	}
	return data, nil
}

func (m *Manager) Verify(id string) error {
	_, err := m.Content(id)
	return err
}

func (m *Manager) Restore(id, target, expectedHash string, validator storage.Validator) error {
	metadata, err := m.readMetadata(id)
	if err != nil {
		return err
	}
	data, err := m.Content(id)
	if err != nil {
		return err
	}
	writer := storage.Writer{Validator: validator, Backup: m}
	owner := metadata.Owner
	return writer.WriteWithOptions(target, data, storage.WriteOptions{
		ExpectedHash: expectedHash,
		Mode:         metadata.Mode,
		Owner:        &owner,
	})
}

func (m *Manager) Prune() error {
	backups, err := m.List()
	if err != nil {
		return err
	}
	ordinary := 0
	for i, metadata := range backups {
		if metadata.Pinned || metadata.Good {
			continue
		}
		ordinary++
		if ordinary <= m.retention || i == 0 {
			continue
		}
		if err := os.Remove(m.dataPath(metadata.ID)); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(m.metadataPath(metadata.ID)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (m *Manager) readMetadata(id string) (Metadata, error) {
	if !idPattern.MatchString(id) || id == "." || id == ".." {
		return Metadata{}, fmt.Errorf("backup: invalid id %q", id)
	}
	data, err := os.ReadFile(m.metadataPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, err
	}
	if metadata.ID != id {
		return Metadata{}, fmt.Errorf("backup: metadata id mismatch")
	}
	return metadata, nil
}

func (m *Manager) read(id string) (Metadata, []byte, error) {
	metadata, err := m.readMetadata(id)
	if err != nil {
		return Metadata{}, nil, err
	}
	data, err := os.ReadFile(m.dataPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{}, nil, ErrNotFound
	}
	return metadata, data, err
}

func (m *Manager) dataPath(id string) string {
	return filepath.Join(m.dir, id)
}

func (m *Manager) metadataPath(id string) string {
	return filepath.Join(m.dir, id+".json")
}
