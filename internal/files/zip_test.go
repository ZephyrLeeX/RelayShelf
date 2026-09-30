package files

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
)

func TestArchiveNames(t *testing.T) {
	used := map[string]bool{}
	for _, tt := range []struct{ in, want string }{
		{"../../报告.txt", "报告.txt"}, {`C:\secret\report.txt`, "report.txt"}, {"report.txt", "report (2).txt"}, {"report (2).txt", "report (2) (2).txt"}, {"REPORT.TXT", "REPORT (3).TXT"}, {"../..", "attachment"}, {"CON.txt", "_CON.txt"}, {"lpt1", "_lpt1"}, {strings.Repeat("中", 100), strings.Repeat("中", 60)}, {"a\x00\r\n:b.txt", "a____b.txt"}, {"a____b.txt", "a____b (2).txt"},
	} {
		if got := archiveName(tt.in, used); got != tt.want {
			t.Errorf("%q: got %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestStreamArchiveContentsAndFailures(t *testing.T) {
	data := []byte(strings.Repeat("contents", 20000))
	hash := sha256.Sum256(data)
	d := Download{Filename: "../中文.txt", Size: int64(len(data)), SHA: hash[:]}
	filePath := t.TempDir() + "/object"
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "open", "read", "short", "hash", "cancel", "write"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var opened, closed int
			var output bytes.Buffer
			item := d
			if mode == "short" {
				item.Size++
			}
			if mode == "hash" {
				item.SHA = []byte("wrong")
			}
			open := func(context.Context, Download) (storage.File, error) {
				if mode == "open" {
					return nil, ErrStorageUnavailable
				}
				f, err := os.Open(filePath)
				if err != nil {
					return nil, err
				}
				opened++
				return &zipTestFile{File: f, mode: mode, cancel: cancel, closed: &closed}, nil
			}
			var dst io.Writer = &output
			if mode == "write" {
				dst = zipFailWriter{}
			}
			err := streamArchive(ctx, dst, []Download{item, item}, open)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				archive, e := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
				if e != nil {
					t.Fatal(e)
				}
				if len(archive.File) != 2 {
					t.Fatal("missing entries")
				}
				for i, f := range archive.File {
					if f.Method != zip.Store || strings.Contains(f.Name, "/") {
						t.Fatal("unsafe entry")
					}
					if i == 1 && f.Name != "中文 (2).txt" {
						t.Fatal(f.Name)
					}
					r, e := f.Open()
					if e != nil {
						t.Fatal(e)
					}
					got, e := io.ReadAll(r)
					r.Close()
					if e != nil || !bytes.Equal(got, data) {
						t.Fatal("wrong bytes", e)
					}
				}
			} else {
				if err == nil {
					t.Fatal("failure reported success")
				}
				if _, e := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len())); e == nil {
					t.Fatal("partial archive is valid")
				}
			}
			if closed != opened {
				t.Fatalf("files open=%d close=%d", opened, closed)
			}
		})
	}
}

type zipTestFile struct {
	storage.File
	mode   string
	cancel context.CancelFunc
	closed *int
	reads  int
	once   sync.Once
}

func (f *zipTestFile) Read(p []byte) (int, error) {
	f.reads++
	if f.mode == "read" && f.reads > 1 {
		return 0, errors.New("storage read failure")
	}
	n, e := f.File.Read(p)
	if f.mode == "cancel" {
		f.cancel()
	}
	return n, e
}
func (f *zipTestFile) Close() error { // Count only once, including cancellation close.
	f.once.Do(func() { *f.closed++; _ = f.File.Close() })
	return nil
}

type zipFailWriter struct{}

func (zipFailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestArchiveCancellationClosesBlockedReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backing, err := os.CreateTemp(t.TempDir(), "object")
	if err != nil {
		t.Fatal(err)
	}
	f := &archiveBlockingFile{File: backing, reading: make(chan struct{}), closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- streamArchive(ctx, &bytes.Buffer{}, []Download{{Filename: "file", Size: 1}}, func(context.Context, Download) (storage.File, error) { return f, nil })
	}()
	select {
	case <-f.reading:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("reader not released")
	}
	select {
	case <-f.closed:
	default:
		t.Fatal("file still open")
	}
}

type archiveBlockingFile struct {
	storage.File
	reading, closed chan struct{}
	once            sync.Once
}

func (f *archiveBlockingFile) Read([]byte) (int, error) {
	close(f.reading)
	<-f.closed
	return 0, os.ErrClosed
}
func (f *archiveBlockingFile) Close() error {
	f.once.Do(func() { _ = f.File.Close(); close(f.closed) })
	return nil
}
