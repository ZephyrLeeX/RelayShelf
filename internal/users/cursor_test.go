package users

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	original := Cursor{CreatedAt: time.Date(2026, 8, 28, 9, 30, 0, 123456789, time.UTC), ID: uuid.Must(uuid.NewV7())}
	decoded, err := DecodeCursor(EncodeCursor(original))
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) || decoded.ID != original.ID {
		t.Fatalf("round trip mismatch: %+v -> %+v", original, decoded)
	}
}

func TestDecodeCursorRejectsMalformedInput(t *testing.T) {
	valid := EncodeCursor(Cursor{CreatedAt: time.Now().UTC(), ID: uuid.Must(uuid.NewV7())})
	for _, value := range []string{
		"",
		"not-base64!!!",
		strings.Repeat("A", 8),
		"e30",
	} {
		if _, err := DecodeCursor(value); err == nil {
			t.Fatalf("decoded malformed cursor %q", value)
		}
	}
	if _, err := DecodeCursor(strings.ToLower(valid[:len(valid)-4]) + "AAAA"); err == nil {
		t.Fatal("decoded corrupted cursor")
	}
}
