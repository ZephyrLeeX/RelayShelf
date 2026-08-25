package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

const SessionTokenBytes = 32

func NewSessionToken() (encoded string, hash [sha256.Size]byte, err error) {
	raw := make([]byte, SessionTokenBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", hash, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), sha256.Sum256(raw), nil
}

func HashSessionToken(encoded string) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != SessionTokenBytes {
		return zero, errors.New("invalid session token")
	}
	return sha256.Sum256(raw), nil
}
