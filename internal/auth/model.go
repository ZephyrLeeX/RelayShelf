package auth

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
)

const (
	IdleLifetime     = 30 * 24 * time.Hour
	AbsoluteLifetime = 90 * 24 * time.Hour
	TouchInterval    = time.Hour
)

type User struct {
	ID                                          uuid.UUID
	Username, DisplayName, PasswordHash, Status string
	IsAdmin                                     bool
}
type Device struct {
	ID, UserID              uuid.UUID
	Name, UserAgent         string
	FirstSeenAt, LastSeenAt time.Time
}
type Session struct {
	ID, UserID, DeviceID                                uuid.UUID
	CreatedAt, LastSeenAt, ExpiresAt, AbsoluteExpiresAt time.Time
	LastIP                                              string
	RevokedAt                                           *time.Time
}
type Authentication struct {
	User    User
	Device  Device
	Session Session
}
type LoginInput struct {
	Username, Password, DeviceName, UserAgent string
	DeviceID                                  *uuid.UUID
	ClientIP                                  netip.Addr
	TraceID                                   string
}
type LoginResult struct {
	Authentication
	RawToken  string
	Challenge *LoginChallenge
}

func (a Authentication) Valid(now time.Time) error {
	if a.User.Status != "ACTIVE" {
		return ErrAuthenticationRequired
	}
	if a.Session.RevokedAt != nil {
		return ErrAuthenticationRequired
	}
	if !now.Before(a.Session.ExpiresAt) || !now.Before(a.Session.AbsoluteExpiresAt) {
		return ErrSessionExpired
	}
	if a.Device.UserID != a.User.ID || a.Session.UserID != a.User.ID || a.Session.DeviceID != a.Device.ID {
		return ErrAuthenticationRequired
	}
	return nil
}
