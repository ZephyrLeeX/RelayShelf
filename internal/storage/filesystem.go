package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
)

type FilesystemStorageAdapter struct{ root string }

func NewFilesystemStorageAdapter(root string) (*FilesystemStorageAdapter, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: root", ErrInvalidKey)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, classify(err)
	}
	if !info.IsDir() {
		return nil, ErrUnavailable
	}
	return &FilesystemStorageAdapter{root: filepath.Clean(root)}, nil
}

func (a *FilesystemStorageAdapter) Root() string { return a.root }

func (a *FilesystemStorageAdapter) path(key Key) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	p := filepath.Join(a.root, filepath.FromSlash(string(key)))
	rel, err := filepath.Rel(a.root, p)
	if err != nil || rel == ".." || filepath.IsAbs(rel) {
		return "", ErrInvalidKey
	}
	return p, nil
}

func (a *FilesystemStorageAdapter) EnsureLayout(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(a.root)
	if err != nil {
		return classify(err)
	}
	if !info.IsDir() {
		return ErrUnavailable
	}
	for _, name := range []string{"objects", "derivatives", ".commit-tmp"} {
		if err = os.Mkdir(filepath.Join(a.root, name), 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			return classify(err)
		}
		if info, err = os.Stat(filepath.Join(a.root, name)); err != nil {
			return classify(err)
		} else if !info.IsDir() {
			return ErrUnavailable
		}
	}
	return a.SameFilesystem(ctx)
}

func (a *FilesystemStorageAdapter) CreateCommitTemp(ctx context.Context, key Key) (File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := a.path(key)
	if err != nil {
		return nil, err
	}
	if filepath.Base(filepath.Dir(p)) != ".commit-tmp" {
		return nil, ErrInvalidKey
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o640)
	if err != nil {
		return nil, classify(err)
	}
	return f, nil
}

func (a *FilesystemStorageAdapter) Open(ctx context.Context, key Key) (File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := a.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, classify(err)
	}
	return f, nil
}

func (a *FilesystemStorageAdapter) Stat(ctx context.Context, key Key) (fs.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := a.path(key)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, classify(err)
	}
	return info, nil
}

func (a *FilesystemStorageAdapter) Commit(ctx context.Context, temp, final Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t, err := a.path(temp)
	if err != nil {
		return err
	}
	f, err := a.path(final)
	if err != nil {
		return err
	}
	if filepath.Base(filepath.Dir(t)) != ".commit-tmp" || filepath.Base(filepath.Dir(f)) != "objects" {
		return ErrInvalidKey
	}
	if _, err = os.Stat(f); err == nil {
		return fmt.Errorf("%w: destination exists", ErrIntegrity)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return classify(err)
	}
	if err = os.Rename(t, f); err != nil {
		return classify(err)
	}
	dir, err := os.Open(filepath.Dir(f))
	if err != nil {
		return classify(err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return classify(syncErr)
	}
	return classify(closeErr)
}

func (a *FilesystemStorageAdapter) Delete(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := a.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return classify(err)
}

func (a *FilesystemStorageAdapter) Space(ctx context.Context) (Space, error) {
	if err := ctx.Err(); err != nil {
		return Space{}, err
	}
	var s syscall.Statfs_t
	if err := syscall.Statfs(a.root, &s); err != nil {
		return Space{}, classify(err)
	}
	return Space{AvailableBytes: s.Bavail * uint64(s.Bsize), TotalBytes: s.Blocks * uint64(s.Bsize)}, nil
}

func (a *FilesystemStorageAdapter) SameFilesystem(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var objects, temp syscall.Stat_t
	if err := syscall.Stat(filepath.Join(a.root, "objects"), &objects); err != nil {
		return classify(err)
	}
	if err := syscall.Stat(filepath.Join(a.root, ".commit-tmp"), &temp); err != nil {
		return classify(err)
	}
	if objects.Dev != temp.Dev {
		return ErrDifferentFilesystems
	}
	return nil
}

func (a *FilesystemStorageAdapter) ListCommitTemps(ctx context.Context) ([]uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(a.root, ".commit-tmp"))
	if err != nil {
		return nil, classify(err)
	}
	out := []uuid.UUID{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if id, ok := parseInternalTempName(entry.Name()); ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w", ErrNotFound)
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return fmt.Errorf("%w", ErrFull)
	}
	return fmt.Errorf("%w", ErrUnavailable)
}
