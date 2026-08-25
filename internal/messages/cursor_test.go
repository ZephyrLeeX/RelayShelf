package messages

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestCursorRoundTripAndInvalid(t *testing.T) {
	want := Cursor{At: time.Date(2026, 8, 25, 1, 2, 3, 456, time.UTC), ID: uuid.New()}
	got, err := DecodeCursor(EncodeCursor(want))
	if err != nil || !got.At.Equal(want.At) || got.ID != want.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for _, value := range []string{"", "%%%", "e30"} {
		if _, err = DecodeCursor(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
func TestPreviewByteAndUTF8Boundary(t *testing.T) {
	body := strings.Repeat("a", MaxPreviewBytes-1) + "界tail"
	value, truncated := preview(body)
	if !truncated || value == nil || len(*value) > MaxPreviewBytes || !utf8.ValidString(*value) {
		t.Fatalf("preview len=%d truncated=%v", len(*value), truncated)
	}
	short := "   "
	value, truncated = preview(short)
	if truncated || value == nil || *value != short {
		t.Fatal("short whitespace body changed")
	}
}
