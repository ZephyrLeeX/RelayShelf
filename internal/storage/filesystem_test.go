package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestFilesystemContract(t *testing.T) {
	root := t.TempDir()
	a, err := NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = a.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"objects", "derivatives", ".commit-tmp"} {
		if info, statErr := os.Stat(filepath.Join(root, dir)); statErr != nil || !info.IsDir() {
			t.Fatalf("layout %s: %v", dir, statErr)
		}
	}
	id := uuid.Must(uuid.NewV7())
	temp, final := CommitTempKey(id), ObjectKey(id)
	f, err := a.CreateCommitTemp(ctx, temp)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("abcdef")
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = a.Commit(ctx, temp, final); err != nil {
		t.Fatal(err)
	}
	info, err := a.Stat(ctx, final)
	if err != nil || info.Size() != int64(len(data)) {
		t.Fatalf("stat: %v %+v", err, info)
	}
	r, err := a.Open(ctx, final)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 2)
	if _, err = io.ReadFull(r, got); err != nil || string(got) != "cd" {
		t.Fatalf("range %q %v", got, err)
	}
	_ = r.Close()
	if err = a.Delete(ctx, final); err != nil {
		t.Fatal(err)
	}
	if err = a.Delete(ctx, final); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidKeysCannotEscape(t *testing.T) {
	a, err := NewFilesystemStorageAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []Key{"/etc/passwd", "../escape", "objects/../../escape", "arbitrary/name"} {
		if _, err = a.Open(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("key %q: %v", key, err)
		}
	}
}

func TestCheckCleansProbe(t *testing.T) {
	root := t.TempDir()
	a, err := NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Check(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"objects", ".commit-tmp"} {
		entries, readErr := os.ReadDir(filepath.Join(root, dir))
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("probe cleanup %s: %v entries=%d", dir, readErr, len(entries))
		}
	}
}
