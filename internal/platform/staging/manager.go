package staging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const suffix = ".upload"

type File interface {
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Stat() (fs.FileInfo, error)
	Close() error
}

type Provider interface {
	Create(uuid.UUID, int64) error
	Open(uuid.UUID) (File, error)
	Stat(uuid.UUID) (fs.FileInfo, error)
	Sync(uuid.UUID) error
	Delete(uuid.UUID) error
	Exists(uuid.UUID) (bool, error)
	OwnedFiles() ([]uuid.UUID, error)
}

type Manager struct{ root string }

func New(root string) (*Manager, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: root must be absolute", ErrUnavailable)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: initialize root", ErrUnavailable)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: root is not a directory", ErrUnavailable)
	}
	return &Manager{root: filepath.Clean(root)}, nil
}

func (m *Manager) path(id uuid.UUID) (string, error) {
	if id == uuid.Nil {
		return "", ErrInvalidUploadID
	}
	return filepath.Join(m.root, id.String()+suffix), nil
}

func (m *Manager) Create(id uuid.UUID, size int64) error {
	if size < 0 {
		return ErrUnavailable
	}
	path, err := m.path(id)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create", ErrUnavailable)
	}
	defer func() { _ = f.Close() }()
	if err = f.Truncate(size); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("%w: size", ErrUnavailable)
	}
	return nil
}

func (m *Manager) Open(id uuid.UUID) (File, error) {
	path, err := m.path(id)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: open", ErrUnavailable)
	}
	return f, nil
}

func (m *Manager) Stat(id uuid.UUID) (fs.FileInfo, error) {
	path, err := m.path(id)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: stat", ErrUnavailable)
	}
	return info, nil
}

func (m *Manager) Sync(id uuid.UUID) error {
	f, err := m.Open(id)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err = f.Sync(); err != nil {
		return fmt.Errorf("%w: sync", ErrUnavailable)
	}
	return nil
}

func (m *Manager) Delete(id uuid.UUID) error {
	path, err := m.path(id)
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: delete", ErrUnavailable)
	}
	return nil
}

func (m *Manager) Exists(id uuid.UUID) (bool, error) {
	_, err := m.Stat(id)
	if err == nil {
		return true, nil
	}
	path, pathErr := m.path(id)
	if pathErr != nil {
		return false, pathErr
	}
	if _, rawErr := os.Stat(path); errors.Is(rawErr, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (m *Manager) OwnedFiles() ([]uuid.UUID, error) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil, fmt.Errorf("%w: scan", ErrUnavailable)
	}
	ids := make([]uuid.UUID, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), suffix)
		id, parseErr := uuid.Parse(base)
		if parseErr == nil && id.String() == base {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
