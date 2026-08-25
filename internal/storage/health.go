package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

type CheckResult struct {
	Root  string
	Space Space
}

func Check(ctx context.Context, a *FilesystemStorageAdapter) (result CheckResult, err error) {
	if err = a.EnsureLayout(ctx); err != nil {
		return result, err
	}
	id := uuid.Must(uuid.NewV7())
	temp, final := CommitTempKey(id), ObjectKey(id)
	defer func() { _ = a.Delete(context.Background(), temp); _ = a.Delete(context.Background(), final) }()
	f, err := a.CreateCommitTemp(ctx, temp)
	if err != nil {
		return result, err
	}
	payload := []byte("relayshelf-storage-check:" + time.Now().UTC().Format(time.RFC3339Nano))
	if _, err = io.CopyBuffer(f, bytes.NewReader(payload), make([]byte, 128<<10)); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return result, classify(err)
	}
	if err = a.Commit(ctx, temp, final); err != nil {
		return result, err
	}
	info, err := a.Stat(ctx, final)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(payload)) {
		return result, ErrIntegrity
	}
	r, err := a.Open(ctx, final)
	if err != nil {
		return result, err
	}
	got := make([]byte, len(payload))
	_, err = io.ReadFull(r, got)
	if err == nil {
		_, err = r.Seek(1, io.SeekStart)
	}
	if err == nil {
		one := make([]byte, 1)
		_, err = io.ReadFull(r, one)
	}
	closeErr := r.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || !bytes.Equal(got, payload) {
		return result, ErrIntegrity
	}
	if err = a.Delete(ctx, final); err != nil {
		return result, err
	}
	if _, statErr := a.Stat(ctx, final); !errors.Is(statErr, ErrNotFound) {
		return result, fmt.Errorf("%w: probe deletion", ErrIntegrity)
	}
	space, err := a.Space(ctx)
	if err != nil {
		return result, err
	}
	return CheckResult{Root: a.Root(), Space: space}, nil
}
