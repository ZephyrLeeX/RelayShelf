package search

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTripAndValidation(t *testing.T) {
	want := Cursor{CreatedAt: time.Date(2026, 8, 26, 10, 11, 12, 345, time.FixedZone("test", 8*60*60)), MessageID: uuid.Must(uuid.NewV7())}
	encoded := EncodeCursor(want)
	got, err := DecodeCursor(encoded)
	if err != nil || !got.CreatedAt.Equal(want.CreatedAt) || got.MessageID != want.MessageID {
		t.Fatalf("decoded=%+v err=%v", got, err)
	}
	invalid := []string{"", "not-base64", base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"a":"2026-01-01T00:00:00Z","i":"00000000-0000-0000-0000-000000000001"}`)), base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"a":"bad","i":"00000000-0000-0000-0000-000000000001"}`)), base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"a":"2026-01-01T00:00:00Z","i":"00000000-0000-0000-0000-000000000000"}`))}
	for _, value := range invalid {
		if _, err = DecodeCursor(value); !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("cursor %q error=%v", value, err)
		}
	}
}

func TestCursorTrailingDataValidation(t *testing.T) {
	validJSON := `{"v":1,"a":"2026-01-01T00:00:00Z","i":"00000000-0000-7000-8000-000000000001"}`
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid cursor JSON", value: base64.RawURLEncoding.EncodeToString([]byte(validJSON))},
		{name: "valid JSON with whitespace", value: base64.RawURLEncoding.EncodeToString([]byte(validJSON + " \n\t"))},
		{name: "second JSON value", value: base64.RawURLEncoding.EncodeToString([]byte(validJSON + `{}`)), wantErr: true},
		{name: "malformed trailing garbage", value: base64.RawURLEncoding.EncodeToString([]byte(validJSON + ` trailing`)), wantErr: true},
		{name: "malformed base64", value: "not-base64!", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeCursor(test.value)
			if test.wantErr && !errors.Is(err, ErrCursorInvalid) {
				t.Fatalf("error=%v want=%v", err, ErrCursorInvalid)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
