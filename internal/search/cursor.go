package search

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

type Cursor struct {
	CreatedAt time.Time
	MessageID uuid.UUID
}

type cursorWire struct {
	Version   int       `json:"v"`
	CreatedAt string    `json:"a"`
	MessageID uuid.UUID `json:"i"`
}

func EncodeCursor(cursor Cursor) string {
	value, _ := json.Marshal(cursorWire{Version: 1, CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano), MessageID: cursor.MessageID})
	return base64.RawURLEncoding.EncodeToString(value)
}

func DecodeCursor(value string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire cursorWire
	if err = decoder.Decode(&wire); err != nil || wire.Version != 1 || wire.MessageID == uuid.Nil {
		return Cursor{}, ErrCursorInvalid
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Cursor{}, ErrCursorInvalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, wire.CreatedAt)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	return Cursor{CreatedAt: createdAt.UTC(), MessageID: wire.MessageID}, nil
}
