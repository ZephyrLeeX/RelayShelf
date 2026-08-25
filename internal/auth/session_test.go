package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionTokenAndCSRF(t *testing.T) {
	encoded, hash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != SessionTokenBytes {
		t.Fatalf("raw length=%d err=%v", len(raw), err)
	}
	if hash != sha256.Sum256(raw) {
		t.Fatal("hash is not SHA-256(raw)")
	}
	parsed, err := HashSessionToken(encoded)
	if err != nil || parsed != hash {
		t.Fatal("encoded token does not round trip")
	}
	if _, err := HashSessionToken(base64.RawURLEncoding.EncodeToString(raw[:31])); err == nil {
		t.Fatal("short token accepted")
	}

	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	one, two := uuid.New(), uuid.New()
	token := csrf.Token(one)
	if !csrf.Verify(one, token) || csrf.Verify(two, token) || csrf.Verify(one, token+"x") {
		t.Fatal("session-bound csrf validation failed")
	}
}

func TestAuthenticationValidity(t *testing.T) {
	now := time.Now()
	uid, did, sid := uuid.New(), uuid.New(), uuid.New()
	base := Authentication{User: User{ID: uid, Status: "ACTIVE"}, Device: Device{ID: did, UserID: uid}, Session: Session{ID: sid, UserID: uid, DeviceID: did, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(2 * time.Hour)}}
	if err := base.Valid(now); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*Authentication)
	}{{"disabled", func(a *Authentication) { a.User.Status = "DISABLED" }}, {"revoked", func(a *Authentication) { a.Session.RevokedAt = &now }}, {"idle expired", func(a *Authentication) { a.Session.ExpiresAt = now }}, {"absolute expired", func(a *Authentication) { a.Session.AbsoluteExpiresAt = now }}, {"device mismatch", func(a *Authentication) { a.Device.UserID = uuid.New() }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			tc.change(&value)
			if err := value.Valid(now); err == nil {
				t.Fatal("invalid authentication accepted")
			}
		})
	}
}
