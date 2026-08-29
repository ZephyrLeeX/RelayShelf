package auth

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	usersdomain "github.com/ZephyrLeeX/RelayShelf/internal/users"
	"github.com/google/uuid"
)

type Clock interface{ Now() time.Time }

type Service struct {
	repo             Repository
	hasher           PasswordHasher
	ids              id.Generator
	clock            Clock
	limiter          *RateLimiter
	totp             *TOTPCipher
	auditMu          sync.Mutex
	lastFailureAudit map[string]time.Time
}

func NewService(repo Repository, hasher PasswordHasher, ids id.Generator, clock Clock, limiter *RateLimiter, totp *TOTPCipher) *Service {
	return &Service{repo: repo, hasher: hasher, ids: ids, clock: clock, limiter: limiter, totp: totp, lastFailureAudit: make(map[string]time.Time)}
}

func validDeviceName(name string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	return n >= 1 && n <= 100
}
func (s *Service) newID() (uuid.UUID, error) { return s.ids.New() }
func (s *Service) event(kind string, actor, target, device, session *uuid.UUID, input LoginInput) (AuditEvent, error) {
	eventID, err := s.newID()
	if err != nil {
		return AuditEvent{}, err
	}
	return AuditEvent{ID: eventID, ActorUserID: actor, EventType: kind, TargetType: "USER", TargetID: target, IP: input.ClientIP, UserAgent: truncate(input.UserAgent, 512), DeviceID: device, SessionID: session, TraceID: truncate(input.TraceID, 128), Metadata: map[string]any{}, CreatedAt: s.clock.Now()}, nil
}

func (s *Service) shouldAuditFailure(key string, now time.Time) bool {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if len(s.lastFailureAudit) >= DefaultRateLimitEntries {
		for k, at := range s.lastFailureAudit {
			if now.Sub(at) > RateEntryTTL {
				delete(s.lastFailureAudit, k)
			}
		}
		if len(s.lastFailureAudit) >= DefaultRateLimitEntries {
			return false
		}
	}
	last := s.lastFailureAudit[key]
	if now.Sub(last) < time.Minute {
		return false
	}
	s.lastFailureAudit[key] = now
	return true
}
func (s *Service) failLogin(ctx context.Context, normalized string, input LoginInput) error {
	s.limiter.Failure(input.ClientIP.String(), normalized)
	if s.shouldAuditFailure(input.ClientIP.String()+"|"+normalized, s.clock.Now()) {
		if event, err := s.event("LOGIN_FAILURE", nil, nil, nil, nil, input); err == nil {
			_ = s.repo.Audit(ctx, event)
		}
	}
	return ErrInvalidCredentials
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	normalized, normalizeErr := usersdomain.NormalizeUsername(input.Username)
	if normalizeErr != nil {
		normalized = "<invalid>"
	}
	if !s.limiter.Allow(input.ClientIP.String(), normalized) {
		return LoginResult{}, ErrRateLimited
	}
	if len(input.Password) > usersdomain.MaxPasswordBytes {
		return LoginResult{}, s.failLogin(ctx, normalized, input)
	}
	if normalizeErr != nil {
		_, _, _ = s.hasher.Verify(DummyHash, input.Password)
		return LoginResult{}, s.failLogin(ctx, normalized, input)
	}
	var err error
	user, findErr := s.repo.FindUser(ctx, normalized)
	if findErr != nil {
		_, _, _ = s.hasher.Verify(DummyHash, input.Password)
		if !errors.Is(findErr, ErrNotFound) {
			return LoginResult{}, findErr
		}
		return LoginResult{}, s.failLogin(ctx, normalized, input)
	}
	if user.Status != "ACTIVE" {
		_, _, _ = s.hasher.Verify(DummyHash, input.Password)
		return LoginResult{}, s.failLogin(ctx, normalized, input)
	}
	ok, needsRehash, verifyErr := s.hasher.Verify(user.PasswordHash, input.Password)
	if verifyErr != nil || !ok {
		return LoginResult{}, s.failLogin(ctx, normalized, input)
	}
	now := s.clock.Now()
	if needsRehash {
		encoded, hashErr := s.hasher.Hash(input.Password)
		if hashErr != nil {
			return LoginResult{}, hashErr
		}
		if err = s.repo.UpdatePasswordHash(ctx, user.ID, encoded, now); err != nil {
			return LoginResult{}, err
		}
		user.PasswordHash = encoded
	}
	// A confirmed TOTP enrollment means a correct password is not a complete
	// authentication. Pending device metadata stays in the bounded challenge;
	// no persistent device is created until the second factor succeeds.
	if _, enabled, totpErr := s.totpEnabled(ctx, user.ID); totpErr != nil {
		return LoginResult{}, totpErr
	} else if enabled {
		if !s.limiter.AllowChallenge(input.ClientIP.String(), normalized) {
			return LoginResult{}, ErrRateLimited
		}
		challenge, challengeErr := s.newLoginChallenge(ctx, user, input)
		if challengeErr != nil {
			return LoginResult{}, challengeErr
		}
		return LoginResult{Challenge: challenge}, nil
	}
	var device Device
	if input.DeviceID != nil {
		device, err = s.repo.GetOwnedDevice(ctx, *input.DeviceID, user.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return LoginResult{}, err
		}
	}
	if input.DeviceID == nil || err != nil {
		name := strings.TrimSpace(input.DeviceName)
		if name == "" {
			name = "New device"
		}
		if !validDeviceName(name) {
			return LoginResult{}, ErrValidation
		}
		deviceID, idErr := s.newID()
		if idErr != nil {
			return LoginResult{}, idErr
		}
		device, err = s.repo.CreateDevice(ctx, Device{ID: deviceID, UserID: user.ID, Name: name, UserAgent: truncate(input.UserAgent, 512), FirstSeenAt: now, LastSeenAt: now})
	}
	if err != nil {
		return LoginResult{}, err
	}
	raw, tokenHash, err := NewSessionToken()
	if err != nil {
		return LoginResult{}, err
	}
	sessionID, err := s.newID()
	if err != nil {
		return LoginResult{}, err
	}
	session := Session{ID: sessionID, UserID: user.ID, DeviceID: device.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(IdleLifetime), AbsoluteExpiresAt: now.Add(AbsoluteLifetime), LastIP: input.ClientIP.String()}
	session, err = s.repo.CreateSession(ctx, session, tokenHash[:], input.ClientIP)
	if err != nil {
		return LoginResult{}, err
	}
	s.limiter.Success(input.ClientIP.String(), normalized)
	if event, eventErr := s.event("LOGIN_SUCCESS", &user.ID, &user.ID, &device.ID, &session.ID, input); eventErr == nil {
		_ = s.repo.Audit(ctx, event)
	}
	user.PasswordHash = ""
	return LoginResult{Authentication: Authentication{User: user, Device: device, Session: session}, RawToken: raw}, nil
}

func truncate(value string, max int) string {
	for len(value) > max {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func (s *Service) Authenticate(ctx context.Context, encoded string, touch bool, ip netip.Addr) (Authentication, error) {
	hash, err := HashSessionToken(encoded)
	if err != nil {
		return Authentication{}, ErrAuthenticationRequired
	}
	authn, err := s.repo.FindAuthentication(ctx, hash[:])
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Authentication{}, ErrAuthenticationRequired
		}
		return Authentication{}, err
	}
	now := s.clock.Now()
	if err = authn.Valid(now); err != nil {
		return Authentication{}, err
	}
	if touch {
		return s.TouchAuthenticated(ctx, authn, ip)
	}
	return authn, nil
}

// ValidateSession re-reads session and user state without extending either
// idle expiry or device activity. Long-lived SSE streams use this path.
func (s *Service) ValidateSession(ctx context.Context, sessionID uuid.UUID) (Authentication, error) {
	authn, err := s.repo.FindAuthenticationBySessionID(ctx, sessionID)
	if err != nil {
		return Authentication{}, ErrAuthenticationRequired
	}
	if err = authn.Valid(s.clock.Now()); err != nil {
		return Authentication{}, err
	}
	return authn, nil
}

// TouchAuthenticated records interactive activity using an authentication that
// has already been loaded and validated. It deliberately performs no token
// parsing or authentication lookup, so callers can place it after request
// integrity checks without duplicating authentication work.
func (s *Service) TouchAuthenticated(ctx context.Context, authn Authentication, ip netip.Addr) (Authentication, error) {
	now := s.clock.Now()
	if err := authn.Valid(now); err != nil {
		return Authentication{}, err
	}
	if now.Sub(authn.Session.LastSeenAt) < TouchInterval {
		return authn, nil
	}
	expires := now.Add(IdleLifetime)
	if expires.After(authn.Session.AbsoluteExpiresAt) {
		expires = authn.Session.AbsoluteExpiresAt
	}
	if err := s.repo.Touch(ctx, authn, now, expires, ip); err != nil {
		return Authentication{}, err
	}
	authn.Session.LastSeenAt, authn.Session.ExpiresAt, authn.Session.LastIP = now, expires, ip.String()
	authn.Device.LastSeenAt = now
	return authn, nil
}

func (s *Service) Revoke(ctx context.Context, actor Authentication, sessionID uuid.UUID, input LoginInput) (bool, error) {
	revoked, err := s.repo.RevokeOwnedSession(ctx, sessionID, actor.User.ID, s.clock.Now())
	if err != nil || !revoked {
		return revoked, err
	}
	event, eventErr := s.event("SESSION_REVOKED", &actor.User.ID, &actor.User.ID, &actor.Device.ID, &sessionID, input)
	if eventErr == nil {
		_ = s.repo.Audit(ctx, event)
	}
	return true, nil
}
func (s *Service) Logout(ctx context.Context, actor Authentication, input LoginInput) error {
	_, err := s.repo.RevokeOwnedSession(ctx, actor.Session.ID, actor.User.ID, s.clock.Now())
	if err != nil {
		return err
	}
	event, eventErr := s.event("LOGOUT", &actor.User.ID, &actor.User.ID, &actor.Device.ID, &actor.Session.ID, input)
	if eventErr == nil {
		_ = s.repo.Audit(ctx, event)
	}
	return nil
}
func (s *Service) ListSessions(ctx context.Context, actor Authentication) ([]Session, error) {
	return s.repo.ListSessions(ctx, actor.User.ID)
}
func (s *Service) ListDevices(ctx context.Context, actor Authentication) ([]Device, error) {
	return s.repo.ListDevices(ctx, actor.User.ID)
}
func (s *Service) RenameDevice(ctx context.Context, actor Authentication, id uuid.UUID, name string) (Device, error) {
	name = strings.TrimSpace(name)
	if !validDeviceName(name) {
		return Device{}, ErrValidation
	}
	return s.repo.RenameDevice(ctx, id, actor.User.ID, name, s.clock.Now())
}

func (s *Service) ChangePassword(ctx context.Context, actor Authentication, current, next string, input LoginInput) error {
	if err := usersdomain.ValidatePassword(next); err != nil {
		return ErrValidation
	}
	user, err := s.repo.FindUserByID(ctx, actor.User.ID)
	if err != nil {
		return err
	}
	ok, _, err := s.hasher.Verify(user.PasswordHash, current)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return err
	}
	event, err := s.event("PASSWORD_CHANGED", &actor.User.ID, &actor.User.ID, &actor.Device.ID, &actor.Session.ID, input)
	if err != nil {
		return err
	}
	return s.repo.ChangePasswordAndRevokeOthers(ctx, actor.User.ID, actor.Session.ID, hash, s.clock.Now(), event)
}
func (s *Service) ResetPasswordByAdmin(ctx context.Context, actor Authentication, targetID uuid.UUID, password string, input LoginInput) error {
	if !actor.User.IsAdmin {
		return ErrForbidden
	}
	if err := usersdomain.ValidatePassword(password); err != nil {
		return ErrValidation
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	// USER_PASSWORD_RESET is a Phase 10 admin event: it is built exclusively by
	// the typed audit constructor, so no caller can attach arbitrary metadata.
	event := audit.UserPasswordReset(audit.Actor{
		UserID:    actor.User.ID,
		DeviceID:  actor.Device.ID,
		SessionID: actor.Session.ID,
		IP:        input.ClientIP,
		UserAgent: truncate(input.UserAgent, 512),
		TraceID:   truncate(input.TraceID, 128),
	}, targetID)
	return s.repo.ResetPasswordAndRevokeAll(ctx, targetID, hash, s.clock.Now(), event)
}
