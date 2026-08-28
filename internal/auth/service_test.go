package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/google/uuid"
)

type sequenceIDs struct{ next int }

func (s *sequenceIDs) New() (uuid.UUID, error) {
	s.next++
	return uuid.MustParse("00000000-0000-7000-8000-" + fmt.Sprintf("%012d", s.next)), nil
}

type memoryRepo struct {
	user        User
	auth        Authentication
	findUserErr error
	devices     map[uuid.UUID]Device
	sessions    []Session
	touchCount  int
	authLookups int
	audits      []AuditEvent
	resetEvent  *audit.Event
	resetTarget uuid.UUID
	resetHash   string
	rehashed    string
	revoked     []uuid.UUID
	totp        *UserTOTP
	challenges  map[string]TOTPChallengeRow
}

func (m *memoryRepo) FindUser(context.Context, string) (User, error) { return m.user, m.findUserErr }
func (m *memoryRepo) FindUserByID(context.Context, uuid.UUID) (User, error) {
	return m.user, m.findUserErr
}
func (m *memoryRepo) UpdatePasswordHash(_ context.Context, _ uuid.UUID, h string, _ time.Time) error {
	m.rehashed = h
	return nil
}
func (m *memoryRepo) GetOwnedDevice(_ context.Context, id, user uuid.UUID) (Device, error) {
	d, ok := m.devices[id]
	if !ok || d.UserID != user {
		return Device{}, ErrNotFound
	}
	return d, nil
}
func (m *memoryRepo) CreateDevice(_ context.Context, d Device) (Device, error) {
	if m.devices == nil {
		m.devices = map[uuid.UUID]Device{}
	}
	m.devices[d.ID] = d
	return d, nil
}
func (m *memoryRepo) CreateSession(_ context.Context, s Session, _ []byte, _ netip.Addr) (Session, error) {
	m.sessions = append(m.sessions, s)
	return s, nil
}
func (m *memoryRepo) FindAuthentication(context.Context, []byte) (Authentication, error) {
	m.authLookups++
	return m.auth, nil
}
func (m *memoryRepo) FindAuthenticationBySessionID(_ context.Context, id uuid.UUID) (Authentication, error) {
	if m.auth.Session.ID == id {
		return m.auth, nil
	}
	return Authentication{}, ErrNotFound
}
func (m *memoryRepo) Touch(_ context.Context, a Authentication, seen, expires time.Time, ip netip.Addr) error {
	m.touchCount++
	m.auth = a
	m.auth.Session.LastSeenAt = seen
	m.auth.Session.ExpiresAt = expires
	return nil
}
func (m *memoryRepo) ListSessions(context.Context, uuid.UUID) ([]Session, error) {
	return m.sessions, nil
}
func (m *memoryRepo) RevokeOwnedSession(_ context.Context, id, user uuid.UUID, _ time.Time) (bool, error) {
	for _, s := range m.sessions {
		if s.ID == id && s.UserID == user {
			m.revoked = append(m.revoked, id)
			return true, nil
		}
	}
	return false, nil
}
func (m *memoryRepo) ListDevices(_ context.Context, user uuid.UUID) ([]Device, error) {
	out := []Device{}
	for _, d := range m.devices {
		if d.UserID == user {
			out = append(out, d)
		}
	}
	return out, nil
}
func (m *memoryRepo) RenameDevice(_ context.Context, id, user uuid.UUID, name string, _ time.Time) (Device, error) {
	d, e := m.GetOwnedDevice(context.Background(), id, user)
	if e != nil {
		return Device{}, e
	}
	d.Name = name
	m.devices[id] = d
	return d, nil
}
func (m *memoryRepo) ChangePasswordAndRevokeOthers(_ context.Context, _ uuid.UUID, current uuid.UUID, hash string, _ time.Time, event AuditEvent) error {
	m.rehashed = hash
	for _, s := range m.sessions {
		if s.ID != current {
			m.revoked = append(m.revoked, s.ID)
		}
	}
	m.audits = append(m.audits, event)
	return nil
}
func (m *memoryRepo) ResetPasswordAndRevokeAll(_ context.Context, target uuid.UUID, hash string, _ time.Time, event audit.Event) error {
	m.resetHash = hash
	m.resetTarget = target
	m.resetEvent = &event
	for _, s := range m.sessions {
		m.revoked = append(m.revoked, s.ID)
	}
	return nil
}
func (m *memoryRepo) Audit(_ context.Context, e AuditEvent) error {
	m.audits = append(m.audits, e)
	return nil
}

// TOTP-backed memory state lets unit tests drive the second-factor flow.
func (m *memoryRepo) GetUserTOTP(context.Context, uuid.UUID) (UserTOTP, error) {
	if m.totp == nil {
		return UserTOTP{}, ErrNotFound
	}
	return *m.totp, nil
}
func (m *memoryRepo) UpsertPendingTOTP(_ context.Context, enrollment UserTOTP, _ uuid.UUID, _ time.Time) (bool, error) {
	if m.totp != nil && m.totp.EnabledAt != nil {
		return false, nil
	}
	m.totp = &enrollment
	return true, nil
}
func (m *memoryRepo) ConfirmTOTP(_ context.Context, user uuid.UUID, at time.Time) (bool, error) {
	if m.totp == nil || m.totp.EnabledAt != nil {
		return false, nil
	}
	m.totp.EnabledAt = &at
	return true, nil
}
func (m *memoryRepo) DeleteUserTOTP(context.Context, uuid.UUID) (bool, error) {
	if m.totp == nil || m.totp.EnabledAt == nil {
		return false, nil
	}
	m.totp = nil
	return true, nil
}
func (m *memoryRepo) RecordTOTPSuccess(_ context.Context, _ uuid.UUID, _ time.Time, step int64) error {
	if m.totp != nil {
		m.totp.LastUsedStep = &step
		m.totp.FailedAttempts = 0
	}
	return nil
}
func (m *memoryRepo) RecordTOTPFailure(_ context.Context, _ uuid.UUID, _ time.Time, _ int, _ time.Time) error {
	return nil
}
func (m *memoryRepo) CreateTOTPChallenge(_ context.Context, row TOTPChallengeRow, hash []byte) error {
	if m.challenges == nil {
		m.challenges = map[string]TOTPChallengeRow{}
	}
	m.challenges[string(hash)] = row
	return nil
}
func (m *memoryRepo) GetTOTPChallengeByHash(_ context.Context, hash []byte) (TOTPChallengeRow, error) {
	row, ok := m.challenges[string(hash)]
	if !ok {
		return TOTPChallengeRow{}, ErrNotFound
	}
	return row, nil
}
func (m *memoryRepo) CompleteTOTPLogin(_ context.Context, command CompleteTOTPLoginCommand) (Device, Session, error) {
	if m.totp == nil || m.totp.EnabledAt == nil || (m.totp.LastUsedStep != nil && *m.totp.LastUsedStep >= command.AcceptedStep) {
		return Device{}, Session{}, ErrTOTPCodeInvalid
	}
	for hash, row := range m.challenges {
		if row.ID == command.ChallengeID {
			if row.ConsumedAt != nil || !row.ExpiresAt.After(command.Now) {
				return Device{}, Session{}, ErrTOTPChallengeExpired
			}
			row.ConsumedAt = &command.Now
			m.challenges[hash] = row
			m.totp.LastUsedStep = &command.AcceptedStep
			device := command.Device
			if command.CandidateDeviceID != nil {
				if existing, ok := m.devices[*command.CandidateDeviceID]; ok && existing.UserID == command.UserID {
					device = existing
				}
			}
			if m.devices == nil {
				m.devices = map[uuid.UUID]Device{}
			}
			m.devices[device.ID] = device
			session := command.Session
			session.DeviceID = device.ID
			m.sessions = append(m.sessions, session)
			return device, session, nil
		}
	}
	return Device{}, Session{}, ErrTOTPChallengeExpired
}
func (m *memoryRepo) BumpTOTPChallengeAttempts(_ context.Context, id uuid.UUID) error {
	for hash, row := range m.challenges {
		if row.ID == id {
			row.Attempts++
			m.challenges[hash] = row
		}
	}
	return nil
}
func (m *memoryRepo) RecordAuditEvent(context.Context, audit.Event) error { return nil }

type countingHasher struct {
	verifies []string
	ok       bool
}

func (h *countingHasher) Hash(string) (string, error) { return "new-hash", nil }
func (h *countingHasher) Verify(encoded, password string) (bool, bool, error) {
	h.verifies = append(h.verifies, encoded)
	return h.ok, false, nil
}

func validAuthentication(now time.Time) Authentication {
	uid, did, sid := uuid.New(), uuid.New(), uuid.New()
	return Authentication{User: User{ID: uid, Username: "alice", PasswordHash: "hash", Status: "ACTIVE"}, Device: Device{ID: did, UserID: uid}, Session: Session{ID: sid, UserID: uid, DeviceID: did, CreatedAt: now.Add(-24 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(60 * 24 * time.Hour)}}
}

func TestUnknownUserRunsDummyVerify(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	repo := &memoryRepo{findUserErr: ErrNotFound}
	hasher := &countingHasher{}
	service := NewService(repo, hasher, &sequenceIDs{}, clock, NewRateLimiter(clock, 10), nil)
	_, err := service.Login(context.Background(), LoginInput{Username: "missing", Password: "password", ClientIP: netip.MustParseAddr("192.0.2.1")})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if len(hasher.verifies) != 1 || hasher.verifies[0] != DummyHash {
		t.Fatalf("verifies=%v", hasher.verifies)
	}
}

func TestAuthenticateTouchThrottleAndNonTouch(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	authn := validAuthentication(clock.now)
	repo := &memoryRepo{auth: authn}
	service := NewService(repo, testHasher(), &sequenceIDs{}, clock, NewRateLimiter(clock, 10), nil)
	token, _, _ := NewSessionToken()
	if _, err := service.Authenticate(context.Background(), token, false, netip.MustParseAddr("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	if repo.touchCount != 0 {
		t.Fatal("non-touch authentication wrote activity")
	}
	authn.Session.LastSeenAt = clock.now.Add(-30 * time.Minute)
	repo.auth = authn
	if _, err := service.Authenticate(context.Background(), token, true, netip.MustParseAddr("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	if repo.touchCount != 0 {
		t.Fatal("throttled authentication wrote activity")
	}
	authn.Session.LastSeenAt = clock.now.Add(-2 * time.Hour)
	repo.auth = authn
	got, err := service.Authenticate(context.Background(), token, true, netip.MustParseAddr("192.0.2.1"))
	if err != nil {
		t.Fatal(err)
	}
	if repo.touchCount != 1 {
		t.Fatalf("touches=%d", repo.touchCount)
	}
	if !got.Session.ExpiresAt.Equal(clock.now.Add(IdleLifetime)) {
		t.Fatal("idle expiry not slid")
	}
	if !got.Session.AbsoluteExpiresAt.Equal(authn.Session.AbsoluteExpiresAt) {
		t.Fatal("absolute expiry changed")
	}
}

func TestPasswordChangeAndAdminResetRevocation(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	actor := validAuthentication(clock.now)
	actor.User.IsAdmin = true
	repo := &memoryRepo{sessions: []Session{actor.Session, {ID: uuid.New(), UserID: actor.User.ID}}}
	hasher := &countingHasher{ok: true}
	service := NewService(repo, hasher, &sequenceIDs{}, clock, NewRateLimiter(clock, 10), nil)
	input := LoginInput{}
	if err := service.ChangePassword(context.Background(), actor, "current-password", "new-password", input); err != nil {
		t.Fatal(err)
	}
	if len(repo.revoked) != 1 || repo.revoked[0] == actor.Session.ID {
		t.Fatalf("revoked=%v", repo.revoked)
	}
	repo.revoked = nil
	target := uuid.New()
	input = LoginInput{ClientIP: netip.MustParseAddr("192.0.2.9"), UserAgent: "admin-test", TraceID: "trace-1"}
	if err := service.ResetPasswordByAdmin(context.Background(), actor, target, "admin-reset", input); err != nil {
		t.Fatal(err)
	}
	if len(repo.revoked) != 2 {
		t.Fatalf("admin reset revoked %d", len(repo.revoked))
	}
	if repo.resetHash == "" || repo.resetTarget != target {
		t.Fatalf("reset not applied to target: hash=%q target=%v", repo.resetHash, repo.resetTarget)
	}
	event := repo.resetEvent
	if event == nil {
		t.Fatal("admin reset recorded no typed audit event")
	}
	if event.Type != audit.EventUserPasswordReset || event.TargetType != "USER" || event.TargetID != target {
		t.Fatalf("typed reset event mismatch: %+v", event)
	}
	if event.Actor.UserID != actor.User.ID || event.Actor.DeviceID != actor.Device.ID || event.Actor.SessionID != actor.Session.ID || event.Actor.IP != input.ClientIP || event.Actor.UserAgent != "admin-test" || event.Actor.TraceID != "trace-1" {
		t.Fatalf("reset audit actor mismatch: %+v", event.Actor)
	}
}
