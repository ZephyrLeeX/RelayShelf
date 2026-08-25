package storage

import (
	"context"
	"io"
	"io/fs"

	"github.com/google/uuid"
)

type File interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Writer
	Sync() error
	Stat() (fs.FileInfo, error)
	Close() error
}

type Space struct{ AvailableBytes, TotalBytes uint64 }

type Adapter interface {
	EnsureLayout(context.Context) error
	CreateCommitTemp(context.Context, Key) (File, error)
	Open(context.Context, Key) (File, error)
	Stat(context.Context, Key) (fs.FileInfo, error)
	Commit(context.Context, Key, Key) error
	Delete(context.Context, Key) error
	Space(context.Context) (Space, error)
	SameFilesystem(context.Context) error
	ListCommitTemps(context.Context) ([]uuid.UUID, error)
}
