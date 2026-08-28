package files

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		value               string
		size, start, length int64
		ok                  bool
	}{{"bytes=0-0", 10, 0, 1, true}, {"bytes=2-5", 10, 2, 4, true}, {"bytes=7-", 10, 7, 3, true}, {"bytes=-3", 10, 7, 3, true}, {"bytes=10-", 10, 0, 0, false}, {"bytes=0-1,3-4", 10, 0, 0, false}}
	for _, tt := range tests {
		start, length, _, err := parseRange(tt.value, tt.size)
		if (err == nil) != tt.ok || tt.ok && (start != tt.start || length != tt.length) {
			t.Errorf("%s got %d,%d,%v", tt.value, start, length, err)
		}
	}
}
func TestContentDisposition(t *testing.T) {
	got := contentDisposition("attachment", "a\"\r\n中.txt")
	if got == "" {
		t.Fatal("empty")
	}
	for _, bad := range []string{"\r", "\n"} {
		for _, r := range bad {
			if containsRune(got, r) {
				t.Fatalf("unsafe %q", got)
			}
		}
	}
}

func TestPreviewMIMEAllowlist(t *testing.T) {
	for _, mime := range []string{"image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf", "audio/mpeg", "audio/mp4", "audio/ogg", "audio/wav", "audio/webm", "video/mp4", "video/webm", "video/ogg", "video/quicktime"} {
		if !isPreviewMIME(mime) {
			t.Errorf("expected %s to be previewable", mime)
		}
	}
	for _, mime := range []string{"text/html", "application/xhtml+xml", "image/svg+xml", "application/xml", "text/xml", "application/octet-stream"} {
		if isPreviewMIME(mime) {
			t.Errorf("unsafe MIME %s was allowlisted", mime)
		}
	}
}
func containsRune(s string, r rune) bool {
	for _, v := range s {
		if v == r {
			return true
		}
	}
	return false
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestCopyDownloadCancellationAndStorageReadFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copyDownload(ctx, &bytes.Buffer{}, bytes.NewReader([]byte("data")), make([]byte, 2)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	readErr := errors.New("injected storage read failure")
	if _, err := copyDownload(context.Background(), &bytes.Buffer{}, failingReader{err: readErr}, make([]byte, 2)); !errors.Is(err, readErr) {
		t.Fatalf("storage read failure=%v", err)
	}
}
