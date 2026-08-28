package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/google/uuid"
)

const (
	// TOTPChallengeLifetime bounds the password-verified second-factor window.
	TOTPChallengeLifetime = 5 * time.Minute
	// MaxTOTPChallengeAttempts bounds verification attempts per challenge.
	MaxTOTPChallengeAttempts = 5
	// TOTPLockoutDuration applies after repeated code failures.
	TOTPLockoutDuration = 5 * time.Minute
	// MaxTOTPFailedAttempts locks an enrollment after repeated bad codes.
	MaxTOTPFailedAttempts = 5
)

// LoginChallenge is the server-side state behind a TOTP second-factor
// challenge. The raw token is returned to the client exactly once; only its
// SHA-256 hash is persisted. A challenge carries no role and no session — it
// can only finish this specific login.
type LoginChallenge struct {
	Token     string
	ExpiresAt time.Time
	User      User
	Device    Device
}

// UserTOTP is the stored enrollment row. SecretCiphertext stays opaque here;
// only the service decrypts it at the point of validation.
type UserTOTP struct {
	UserID                  uuid.UUID
	SecretCiphertext        []byte
	SecretNonce             []byte
	SecretEncryptionVersion int16
	EnabledAt               *time.Time
	LastUsedStep            *int64
	FailedAttempts          int
	LockedUntil             *time.Time
}

// TOTPChallengeRow is a persisted login challenge.
type TOTPChallengeRow struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DeviceID   *uuid.UUID
	ExpiresAt  time.Time
	Attempts   int
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// totpEnabled reports whether the user must complete a second factor.
func (s *Service) totpEnabled(ctx context.Context, userID uuid.UUID) (UserTOTP, bool, error) {
	row, err := s.repo.GetUserTOTP(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return UserTOTP{}, false, nil
	}
	if err != nil {
		return UserTOTP{}, false, err
	}
	return row, row.EnabledAt != nil, nil
}

// newLoginChallenge persists a single-use challenge capability after a valid
// password verification and returns it with its raw bearer token.
func (s *Service) newLoginChallenge(ctx context.Context, user User, device Device) (*LoginChallenge, error) {
	raw := make([]byte, SessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	challenge := &LoginChallenge{Token: token, ExpiresAt: now.Add(TOTPChallengeLifetime), User: user, Device: device}
	var deviceID *uuid.UUID
	if device.ID != uuid.Nil {
		value := device.ID
		deviceID = &value
	}
	if err = s.repo.CreateTOTPChallenge(ctx, TOTPChallengeRow{ID: id, UserID: user.ID, DeviceID: deviceID, ExpiresAt: challenge.ExpiresAt, CreatedAt: now}, hash[:]); err != nil {
		return nil, err
	}
	return challenge, nil
}

// CompleteLoginTOTP exchanges a challenge token plus a valid TOTP code for a
// fully authenticated session. Password-only possession of the challenge is
// never enough, and the challenge dies with this attempt sequence.
func (s *Service) CompleteLoginTOTP(ctx context.Context, token, code string, input LoginInput) (LoginResult, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != SessionTokenBytes {
		_, _, _ = s.hasher.Verify(DummyHash, "totp-dummy-password")
		return LoginResult{}, ErrInvalidCredentials
	}
	hash := sha256.Sum256(raw)
	challenge, err := s.repo.GetTOTPChallengeByHash(ctx, hash[:])
	if errors.Is(err, ErrNotFound) {
		_, _, _ = s.hasher.Verify(DummyHash, "totp-dummy-password")
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	now := s.clock.Now()
	if challenge.ConsumedAt != nil || !challenge.ExpiresAt.After(now) {
		return LoginResult{}, ErrTOTPChallengeExpired
	}
	if challenge.Attempts >= MaxTOTPChallengeAttempts {
		return LoginResult{}, ErrRateLimited
	}
	user, err := s.repo.FindUserByID(ctx, challenge.UserID)
	if err != nil {
		return LoginResult{}, err
	}
	if !s.limiter.Allow(input.ClientIP.String(), user.Username) {
		return LoginResult{}, ErrRateLimited
	}
	if user.Status != "ACTIVE" {
		s.limiter.Failure(input.ClientIP.String(), user.Username)
		return LoginResult{}, ErrInvalidCredentials
	}
	enrollment, enabled, err := s.totpEnabled(ctx, user.ID)
	if err != nil {
		return LoginResult{}, err
	}
	if !enabled {
		return LoginResult{}, ErrInvalidCredentials
	}
	if enrollment.LockedUntil != nil && enrollment.LockedUntil.After(now) {
		return LoginResult{}, ErrRateLimited
	}
	secret, decryptErr := s.totp.Decrypt(user.ID, enrollment.SecretEncryptionVersion, enrollment.SecretNonce, enrollment.SecretCiphertext)
	if decryptErr != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	var lastStep int64 = -1
	if enrollment.LastUsedStep != nil {
		lastStep = *enrollment.LastUsedStep
	}
	step, validateErr := ValidateTOTP(secret, code, now.Unix(), lastStep)
	if validateErr != nil {
		s.recordTOTPFailure(ctx, user.ID, challenge.ID, now)
		s.limiter.Failure(input.ClientIP.String(), user.Username)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err = s.repo.RecordTOTPSuccess(ctx, user.ID, now, step); err != nil {
		return LoginResult{}, err
	}
	consumed, err := s.repo.ConsumeTOTPChallenge(ctx, challenge.ID, now)
	if err != nil {
		return LoginResult{}, err
	}
	if !consumed {
		return LoginResult{}, ErrTOTPChallengeExpired
	}
	device, err := s.challengeDevice(ctx, user, challenge)
	if err != nil {
		return LoginResult{}, err
	}
	raw2, tokenHash, err := NewSessionToken()
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
	s.limiter.Success(input.ClientIP.String(), user.Username)
	if event, eventErr := s.event("LOGIN_SUCCESS", &user.ID, &user.ID, &device.ID, &session.ID, input); eventErr == nil {
		_ = s.repo.Audit(ctx, event)
	}
	user.PasswordHash = ""
	return LoginResult{Authentication: Authentication{User: user, Device: device, Session: session}, RawToken: raw2}, nil
}

func (s *Service) challengeDevice(ctx context.Context, user User, challenge TOTPChallengeRow) (Device, error) {
	if challenge.DeviceID != nil {
		device, err := s.repo.GetOwnedDevice(ctx, *challenge.DeviceID, user.ID)
		if err == nil {
			return device, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Device{}, err
		}
	}
	deviceID, err := s.newID()
	if err != nil {
		return Device{}, err
	}
	now := s.clock.Now()
	return s.repo.CreateDevice(ctx, Device{ID: deviceID, UserID: user.ID, Name: "New device", UserAgent: "", FirstSeenAt: now, LastSeenAt: now})
}

func (s *Service) recordTOTPFailure(ctx context.Context, userID, challengeID uuid.UUID, now time.Time) {
	_ = s.repo.RecordTOTPFailure(ctx, userID, now, MaxTOTPFailedAttempts, now.Add(TOTPLockoutDuration))
	_ = s.repo.BumpTOTPChallengeAttempts(ctx, challengeID)
}

// TOTPStatus reports whether the authenticated user has a confirmed enrollment.
func (s *Service) TOTPStatus(ctx context.Context, actor Authentication) (bool, error) {
	_, enabled, err := s.totpEnabled(ctx, actor.User.ID)
	return enabled, err
}

// TOTPEnrollment is the pending enrollment material returned exactly once.
type TOTPEnrollment struct {
	Secret        string
	OtpauthURL    string
	Digits        int
	PeriodSeconds int
	Algorithm     string
}

// StartTOTPEnrollment replaces any pending enrollment with a fresh encrypted
// secret. An already confirmed enrollment must be disabled first.
func (s *Service) StartTOTPEnrollment(ctx context.Context, actor Authentication, input LoginInput) (TOTPEnrollment, error) {
	if _, enabled, err := s.totpEnabled(ctx, actor.User.ID); err != nil {
		return TOTPEnrollment{}, err
	} else if enabled {
		return TOTPEnrollment{}, ErrTOTPAlreadyEnabled
	}
	_, encoded, err := GenerateTOTPSecret()
	if err != nil {
		return TOTPEnrollment{}, err
	}
	secret, err := DecodeTOTPSecret(encoded)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	ciphertext, nonce, version, err := s.totp.Encrypt(actor.User.ID, secret)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	id, err := s.newID()
	if err != nil {
		return TOTPEnrollment{}, err
	}
	now := s.clock.Now()
	created, err := s.repo.UpsertPendingTOTP(ctx, UserTOTP{UserID: actor.User.ID, SecretCiphertext: ciphertext, SecretNonce: nonce, SecretEncryptionVersion: version}, id, now)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	if !created {
		return TOTPEnrollment{}, ErrTOTPAlreadyEnabled
	}
	return TOTPEnrollment{Secret: encoded, OtpauthURL: TOTPOtpauthURL(actor.User.Username, encoded), Digits: TOTPDigits, PeriodSeconds: TOTPPeriodSeconds, Algorithm: "SHA1"}, nil
}

// ConfirmTOTPEnrollment promotes the pending enrollment only after the user
// proves the authenticator is configured by presenting a valid code.
func (s *Service) ConfirmTOTPEnrollment(ctx context.Context, actor Authentication, code string, input LoginInput) error {
	row, err := s.repo.GetUserTOTP(ctx, actor.User.ID)
	if errors.Is(err, ErrNotFound) {
		return ErrTOTPNotEnabled
	}
	if err != nil {
		return err
	}
	if row.EnabledAt != nil {
		return ErrTOTPAlreadyEnabled
	}
	now := s.clock.Now()
	if row.LockedUntil != nil && row.LockedUntil.After(now) {
		return ErrRateLimited
	}
	secret, decryptErr := s.totp.Decrypt(actor.User.ID, row.SecretEncryptionVersion, row.SecretNonce, row.SecretCiphertext)
	if decryptErr != nil {
		return ErrTOTPCodeInvalid
	}
	if _, validateErr := ValidateTOTP(secret, code, now.Unix(), -1); validateErr != nil {
		_ = s.repo.RecordTOTPFailure(ctx, actor.User.ID, now, MaxTOTPFailedAttempts, now.Add(TOTPLockoutDuration))
		return ErrTOTPCodeInvalid
	}
	confirmed, err := s.repo.ConfirmTOTP(ctx, actor.User.ID, now)
	if err != nil {
		return err
	}
	if !confirmed {
		return ErrTOTPAlreadyEnabled
	}
	_ = s.repo.RecordTOTPSuccess(ctx, actor.User.ID, now, now.Unix()/TOTPPeriodSeconds)
	s.recordTOTPAudit(ctx, audit.EventTOTPEnrollmentConfirmed, actor, input)
	return nil
}

// DisableTOTP removes a confirmed enrollment after re-presenting a valid code.
// The public-exposure gate, not this method, decides whether an administrator
// may safely remain without TOTP.
func (s *Service) DisableTOTP(ctx context.Context, actor Authentication, code string, input LoginInput) error {
	row, err := s.repo.GetUserTOTP(ctx, actor.User.ID)
	if errors.Is(err, ErrNotFound) {
		return ErrTOTPNotEnabled
	}
	if err != nil {
		return err
	}
	if row.EnabledAt == nil {
		return ErrTOTPNotEnabled
	}
	now := s.clock.Now()
	if row.LockedUntil != nil && row.LockedUntil.After(now) {
		return ErrRateLimited
	}
	secret, decryptErr := s.totp.Decrypt(actor.User.ID, row.SecretEncryptionVersion, row.SecretNonce, row.SecretCiphertext)
	if decryptErr != nil {
		return ErrTOTPCodeInvalid
	}
	var lastStep int64 = -1
	if row.LastUsedStep != nil {
		lastStep = *row.LastUsedStep
	}
	if _, validateErr := ValidateTOTP(secret, code, now.Unix(), lastStep); validateErr != nil {
		_ = s.repo.RecordTOTPFailure(ctx, actor.User.ID, now, MaxTOTPFailedAttempts, now.Add(TOTPLockoutDuration))
		return ErrTOTPCodeInvalid
	}
	deleted, err := s.repo.DeleteUserTOTP(ctx, actor.User.ID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrTOTPNotEnabled
	}
	s.recordTOTPAudit(ctx, audit.EventTOTPDisabled, actor, input)
	return nil
}

// recordTOTPAudit writes the typed, metadata-free TOTP audit events. The
// constructors are the allowlist: secret material and codes have nowhere to go.
func (s *Service) recordTOTPAudit(ctx context.Context, eventType audit.EventType, actor Authentication, input LoginInput) {
	event := audit.TOTPEvent(eventType, audit.Actor{
		UserID:    actor.User.ID,
		DeviceID:  actor.Device.ID,
		SessionID: actor.Session.ID,
		IP:        input.ClientIP,
		UserAgent: truncate(input.UserAgent, 512),
		TraceID:   truncate(input.TraceID, 128),
	}, actor.User.ID)
	_ = s.repo.RecordAuditEvent(ctx, event)
}
