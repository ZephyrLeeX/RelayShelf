package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"github.com/google/uuid"
)

type CSRF struct{ secret []byte }

func NewCSRF(secret []byte) *CSRF { return &CSRF{secret: append([]byte(nil), secret...)} }
func (c *CSRF) Token(sessionID uuid.UUID) string {
	h := hmac.New(sha256.New, c.secret)
	_, _ = h.Write([]byte("relayshelf-csrf-v1:"))
	_, _ = h.Write(sessionID[:])
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (c *CSRF) Verify(sessionID uuid.UUID, token string) bool {
	actual, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return false
	}
	expected, _ := base64.RawURLEncoding.DecodeString(c.Token(sessionID))
	return hmac.Equal(actual, expected)
}
