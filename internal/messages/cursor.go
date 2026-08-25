package messages

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Cursor struct {
	At time.Time
	ID uuid.UUID
}

type cursorWire struct {
	At string    `json:"a"`
	ID uuid.UUID `json:"i"`
}

func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(cursorWire{At: c.At.UTC().Format(time.RFC3339Nano), ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeCursor(value string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, ErrValidation
	}
	var wire cursorWire
	if err = json.Unmarshal(b, &wire); err != nil || wire.ID == uuid.Nil {
		return Cursor{}, ErrValidation
	}
	at, err := time.Parse(time.RFC3339Nano, wire.At)
	if err != nil {
		return Cursor{}, ErrValidation
	}
	return Cursor{At: at.UTC(), ID: wire.ID}, nil
}

func preview(body string) (*string, bool) {
	if len(body) <= MaxPreviewBytes {
		value := body
		return &value, false
	}
	cut := MaxPreviewBytes
	for cut > 0 && (body[cut]&0xc0) == 0x80 {
		cut--
	}
	value := body[:cut]
	return &value, true
}
