//go:build integration

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// stepClock is an advanceable clock so TOTP steps are deterministic.
type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }
func (c *stepClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

type totpHarness struct {
	pool    *pgxpool.Pool
	service *Service
	router  http.Handler
	cipher  *TOTPCipher
	csrf    *CSRF
	clock   *stepClock
}

type blockingGetTOTPRepository struct {
	Repository
	once        sync.Once
	readReached chan struct{}
	resume      chan struct{}
}

func (r *blockingGetTOTPRepository) GetUserTOTP(ctx context.Context, userID uuid.UUID) (UserTOTP, error) {
	row, err := r.Repository.GetUserTOTP(ctx, userID)
	r.once.Do(func() {
		close(r.readReached)
		<-r.resume
	})
	return row, err
}

func newTOTPHarness(t *testing.T) totpHarness {
	t.Helper()
	pool := postgresutil.NewDatabase(t)
	frozen := &stepClock{now: time.Now().UTC().Truncate(time.Second)}
	recorder := audit.NewRecorder(id.UUIDv7{}, frozen)
	repo := NewPostgreSQLRepository(pool, recorder)
	hasher := NewPasswordHasher(Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(3*i + 5)
	}
	cipher, err := NewTOTPCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, hasher, id.UUIDv7{}, frozen, NewRateLimiter(frozen, 4096), cipher)
	csrf := NewCSRF(key)
	origin := &url.URL{Scheme: "http", Host: "relayshelf.test"}
	handler := NewHandler(service, csrf, NewCookiePolicy(origin))
	middleware := NewMiddleware(service, NewCookiePolicy(origin), csrf, origin, httpx.NewResolver(nil))
	return totpHarness{pool: pool, service: service, router: Router(handler, middleware), cipher: cipher, csrf: csrf, clock: frozen}
}

type totpUser struct {
	id       uuid.UUID
	username string
	password string
}

func (h totpHarness) createUser(t *testing.T, username string, isAdmin bool) totpUser {
	t.Helper()
	hasher := NewPasswordHasher(Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	password := "totp-password-123"
	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.Must(uuid.NewV7())
	if _, err = h.pool.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,$3,$4,'ACTIVE')`, userID, username, encoded, isAdmin); err != nil {
		t.Fatal(err)
	}
	return totpUser{id: userID, username: username, password: password}
}

func (h totpHarness) sessionFor(t *testing.T, user totpUser) map[string]string {
	t.Helper()
	result, err := h.service.Login(context.Background(), LoginInput{Username: user.username, Password: user.password, DeviceName: "harness", ClientIP: netip.MustParseAddr("192.0.2.10")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Challenge != nil {
		t.Fatal("user unexpectedly TOTP-gated")
	}
	return map[string]string{"Cookie": "relayshelf_session=" + result.RawToken, "X-CSRF-Token": h.csrf.Token(result.Session.ID)}
}

// enrollAndConfirm inserts a confirmed enrollment using the real cipher, so
// tests never need the plaintext secret except through deterministic codes.
func (h totpHarness) enrollAndConfirm(t *testing.T, user totpUser) []byte {
	t.Helper()
	secret, _, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, version, err := h.cipher.Encrypt(user.id, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.pool.Exec(context.Background(), `INSERT INTO user_totp(id,user_id,secret_ciphertext,secret_nonce,secret_encryption_version,digits,period_seconds,algorithm,enabled_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,6,30,'SHA1',now(),now(),now())`, uuid.Must(uuid.NewV7()), user.id, ciphertext, nonce, version); err != nil {
		t.Fatal(err)
	}
	return secret
}

func (h totpHarness) call(t *testing.T, method, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, "http://relayshelf.test"+path, nil)
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request.Body = io.NopCloser(strings.NewReader(string(encoded)))
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Origin", "http://relayshelf.test")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, request)
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder, decoded
}

func (h totpHarness) code(secret []byte) string {
	return TOTPCode(secret, h.clock.Now().Unix()/TOTPPeriodSeconds, TOTPDigits)
}

func TestTOTPEnrollmentConfirmationAndLogin(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "alice", false)
	headers := h.sessionFor(t, user)

	// Ordinary users may stay without TOTP.
	response, body := h.call(t, http.MethodGet, "/api/v1/auth/totp", nil, headers)
	if response.Code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("status=%d body=%v", response.Code, body)
	}

	// Enrollment returns the secret exactly once.
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("enroll status=%d body=%v", response.Code, body)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("enrollment cache headers=%v", response.Header())
	}
	secret, _ := body["secret"].(string)
	if len(secret) != 32 {
		t.Fatalf("secret length=%d", len(secret))
	}
	if otpauth, _ := body["otpauthUrl"].(string); !strings.HasPrefix(otpauth, "otpauth://totp/RelayShelf%3Aalice?") {
		t.Fatalf("otpauth=%q", otpauth)
	}

	// The stored form must be ciphertext, never the base32 plaintext.
	var storedCiphertext, storedNonce []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT secret_ciphertext, secret_nonce FROM user_totp WHERE user_id=$1`, user.id).Scan(&storedCiphertext, &storedNonce); err != nil {
		t.Fatal(err)
	}
	if len(storedCiphertext) == 0 || len(storedNonce) != 12 || strings.Contains(string(storedCiphertext), secret) {
		t.Fatalf("ciphertext=%d nonce=%d", len(storedCiphertext), len(storedNonce))
	}

	// A pending enrollment does not gate login yet.
	response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "alice", "password": user.password, "deviceName": "d1"}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("pending login status=%d", response.Code)
	}

	// Wrong confirmation codes are rejected; the correct code confirms.
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/confirm", map[string]any{"code": "000000"}, headers); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong confirm status=%d", response.Code)
	}
	secretBytes, err := DecodeTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/totp/confirm", map[string]any{"code": h.code(secretBytes)}, headers)
	if response.Code != http.StatusOK || body["enabled"] != true {
		t.Fatalf("confirm status=%d body=%v", response.Code, body)
	}

	// Password-only login now yields a challenge and never a session cookie.
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "alice", "password": user.password, "deviceName": "d1"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("gated login status=%d body=%v", response.Code, body)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("challenge cache headers=%v", response.Header())
	}
	challengeToken, _ := body["challengeToken"].(string)
	if challengeToken == "" {
		t.Fatal("no challenge token")
	}
	for _, cookie := range response.Header().Values("Set-Cookie") {
		if strings.Contains(cookie, "relayshelf_session=") && !strings.Contains(cookie, "relayshelf_session=; Max-Age=0") {
			t.Fatalf("gated login issued a session cookie: %q", cookie)
		}
	}

	// A wrong code cannot complete the challenge.
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": challengeToken, "code": "000000"}, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong totp status=%d", response.Code)
	}

	// The correct code completes login with a full bootstrap and cookie.
	h.clock.Advance(31 * time.Second)
	code := h.code(secretBytes)
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": challengeToken, "code": code}, nil)
	if response.Code != http.StatusOK || body["csrfToken"] == nil {
		t.Fatalf("complete status=%d body=%v", response.Code, body)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("completion cache headers=%v", response.Header())
	}
	if !strings.Contains(response.Header().Get("Set-Cookie"), "relayshelf_session=") {
		t.Fatalf("no session cookie: %q", response.Header().Get("Set-Cookie"))
	}

	// The challenge is single-use.
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": challengeToken, "code": code}, nil); response.Code != http.StatusUnauthorized && response.Code != http.StatusGone {
		t.Fatalf("challenge replay status=%d", response.Code)
	}

	// The consumed OTP code cannot be replayed through a fresh challenge.
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "alice", "password": user.password, "deviceName": "d1"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("second gated login status=%d", response.Code)
	}
	secondToken, _ := body["challengeToken"].(string)
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": secondToken, "code": code}, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code accepted: status=%d", response.Code)
	}
}

func TestTOTPEnrollmentRequiresCurrentPassword(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "reauth-user", false)
	headers := h.sessionFor(t, user)

	response, _ := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{}, headers)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing password status=%d", response.Code)
	}
	response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": "wrong-password-123"}, headers)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d", response.Code)
	}
	var count int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_totp WHERE user_id=$1`, user.id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("wrong-password enrollment rows=%d err=%v", count, err)
	}

	response, body := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers)
	if response.Code != http.StatusCreated || body["secret"] == nil {
		t.Fatalf("correct password status=%d body=%v", response.Code, body)
	}
	var enabledAt *time.Time
	var ciphertext []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT enabled_at, secret_ciphertext FROM user_totp WHERE user_id=$1`, user.id).Scan(&enabledAt, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if enabledAt != nil || len(ciphertext) == 0 {
		t.Fatalf("pending enrollment enabledAt=%v ciphertext=%d", enabledAt, len(ciphertext))
	}
}

func TestTOTPEnrollmentPasswordReauthIsRateLimited(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "reauth-rate-user", false)
	headers := h.sessionFor(t, user)
	statuses := make([]int, 4)
	for i := range statuses {
		response, _ := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": "wrong-password-123"}, headers)
		statuses[i] = response.Code
	}
	if statuses[0] != http.StatusUnauthorized || statuses[1] != http.StatusUnauthorized || statuses[2] != http.StatusUnauthorized || statuses[3] != http.StatusTooManyRequests {
		t.Fatalf("reauth rate statuses=%v", statuses)
	}
	var count int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_totp WHERE user_id=$1`, user.id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rate-limited enrollment rows=%d err=%v", count, err)
	}
}

func TestTOTPEnrollmentPersistsFutureAcceptedStepAndRejectsReplay(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "future-step-user", false)
	headers := h.sessionFor(t, user)
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("enroll status=%d body=%v", response.Code, body)
	}
	secret, err := DecodeTOTPSecret(body["secret"].(string))
	if err != nil {
		t.Fatal(err)
	}
	futureStep := h.clock.Now().Unix()/TOTPPeriodSeconds + 1
	futureCode := TOTPCode(secret, futureStep, TOTPDigits)
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/totp/confirm", map[string]any{"code": futureCode}, headers)
	if response.Code != http.StatusOK {
		t.Fatalf("future-window confirmation status=%d body=%v", response.Code, body)
	}
	var enabledAt *time.Time
	var lastStep int64
	if err = h.pool.QueryRow(context.Background(), `SELECT enabled_at,last_used_step FROM user_totp WHERE user_id=$1`, user.id).Scan(&enabledAt, &lastStep); err != nil {
		t.Fatal(err)
	}
	if enabledAt == nil || lastStep != futureStep {
		t.Fatalf("enabledAt=%v lastStep=%d want=%d", enabledAt, lastStep, futureStep)
	}
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": user.username, "password": user.password, "deviceName": "replay-device"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("challenge status=%d body=%v", response.Code, body)
	}
	response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": body["challengeToken"], "code": futureCode}, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("future confirmation code replay status=%d", response.Code)
	}
}

func TestTOTPEnrollmentConfirmationMutationRollsBack(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "rollback-user", false)
	headers := h.sessionFor(t, user)
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("enroll status=%d body=%v", response.Code, body)
	}
	secret, err := DecodeTOTPSecret(body["secret"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.pool.Exec(context.Background(), `
CREATE FUNCTION reject_totp_confirmation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.enabled_at IS NULL AND NEW.enabled_at IS NOT NULL THEN
    RAISE EXCEPTION 'injected confirmation failure';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER reject_totp_confirmation BEFORE UPDATE ON user_totp
FOR EACH ROW EXECUTE FUNCTION reject_totp_confirmation()`); err != nil {
		t.Fatal(err)
	}
	response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/confirm", map[string]any{"code": h.code(secret)}, headers)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("injected failure status=%d", response.Code)
	}
	var enabledAt *time.Time
	var lastStep *int64
	if err = h.pool.QueryRow(context.Background(), `SELECT enabled_at,last_used_step FROM user_totp WHERE user_id=$1`, user.id).Scan(&enabledAt, &lastStep); err != nil {
		t.Fatal(err)
	}
	if enabledAt != nil || lastStep != nil {
		t.Fatalf("partial confirmation persisted enabledAt=%v lastStep=%v", enabledAt, lastStep)
	}
}

func TestConcurrentTOTPEnrollmentConfirmationHasOneWinner(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "confirm-race-user", false)
	headers := h.sessionFor(t, user)
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("enroll status=%d body=%v", response.Code, body)
	}
	secret, err := DecodeTOTPSecret(body["secret"].(string))
	if err != nil {
		t.Fatal(err)
	}
	code := h.code(secret)
	start := make(chan struct{})
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			request := httptest.NewRequest(http.MethodPost, "http://relayshelf.test/api/v1/auth/totp/confirm", strings.NewReader(`{"code":"`+code+`"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://relayshelf.test")
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			h.router.ServeHTTP(recorder, request)
			statuses[i] = recorder.Code
		}(i)
	}
	close(start)
	wg.Wait()
	successes, conflicts := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("concurrent confirmation statuses=%v", statuses)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent confirmation statuses=%v", statuses)
	}
	var enabledCount int
	var lastStep int64
	if err = h.pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE enabled_at IS NOT NULL),max(last_used_step) FROM user_totp WHERE user_id=$1`, user.id).Scan(&enabledCount, &lastStep); err != nil {
		t.Fatal(err)
	}
	if enabledCount != 1 || lastStep != h.clock.Now().Unix()/TOTPPeriodSeconds {
		t.Fatalf("enabled=%d lastStep=%d", enabledCount, lastStep)
	}
}

func TestTOTPEnrollmentStaleConfirmationCannotEnableReplacement(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "stale-enrollment-user", false)
	headers := h.sessionFor(t, user)
	rawToken := strings.TrimPrefix(headers["Cookie"], "relayshelf_session=")
	actor, err := h.service.Authenticate(context.Background(), rawToken, false, netip.MustParseAddr("192.0.2.10"))
	if err != nil {
		t.Fatal(err)
	}

	enrollmentA, err := h.service.StartTOTPEnrollment(context.Background(), actor, user.password, LoginInput{ClientIP: netip.MustParseAddr("192.0.2.10")})
	if err != nil {
		t.Fatal(err)
	}
	secretA, err := DecodeTOTPSecret(enrollmentA.Secret)
	if err != nil {
		t.Fatal(err)
	}

	blockingRepo := &blockingGetTOTPRepository{
		Repository:  h.service.repo,
		readReached: make(chan struct{}),
		resume:      make(chan struct{}),
	}
	confirmService := NewService(blockingRepo, h.service.hasher, h.service.ids, h.clock, h.service.limiter, h.cipher)
	confirmResult := make(chan error, 1)
	go func() {
		confirmResult <- confirmService.ConfirmTOTPEnrollment(context.Background(), actor, h.code(secretA), LoginInput{ClientIP: netip.MustParseAddr("192.0.2.10")})
	}()

	select {
	case <-blockingRepo.readReached:
	case <-time.After(5 * time.Second):
		close(blockingRepo.resume)
		t.Fatal("confirmation did not reach the post-read barrier")
	}

	enrollmentB, err := h.service.StartTOTPEnrollment(context.Background(), actor, user.password, LoginInput{ClientIP: netip.MustParseAddr("192.0.2.10")})
	if err != nil {
		close(blockingRepo.resume)
		t.Fatal(err)
	}
	secretB, err := DecodeTOTPSecret(enrollmentB.Secret)
	if err != nil {
		close(blockingRepo.resume)
		t.Fatal(err)
	}
	var bCiphertext, bNonce []byte
	var bVersion int16
	var pendingEnabledAt *time.Time
	if err = h.pool.QueryRow(context.Background(), `SELECT secret_ciphertext,secret_nonce,secret_encryption_version,enabled_at FROM user_totp WHERE user_id=$1`, user.id).Scan(&bCiphertext, &bNonce, &bVersion, &pendingEnabledAt); err != nil {
		close(blockingRepo.resume)
		t.Fatal(err)
	}
	if pendingEnabledAt != nil {
		close(blockingRepo.resume)
		t.Fatalf("replacement enrollment was prematurely enabled at %v", pendingEnabledAt)
	}

	close(blockingRepo.resume)
	select {
	case err = <-confirmResult:
	case <-time.After(5 * time.Second):
		t.Fatal("stale confirmation did not finish")
	}
	if !errors.Is(err, ErrTOTPEnrollmentChanged) {
		t.Fatalf("stale confirmation error=%v", err)
	}

	var actualCiphertext, actualNonce []byte
	var actualVersion int16
	var enabledAt *time.Time
	var lastStep *int64
	var failedAttempts int
	if err = h.pool.QueryRow(context.Background(), `SELECT secret_ciphertext,secret_nonce,secret_encryption_version,enabled_at,last_used_step,failed_attempts FROM user_totp WHERE user_id=$1`, user.id).Scan(&actualCiphertext, &actualNonce, &actualVersion, &enabledAt, &lastStep, &failedAttempts); err != nil {
		t.Fatal(err)
	}
	if enabledAt != nil || lastStep != nil || failedAttempts != 0 {
		t.Fatalf("stale confirmation mutated replacement: enabledAt=%v lastStep=%v failedAttempts=%d", enabledAt, lastStep, failedAttempts)
	}
	if !bytes.Equal(actualCiphertext, bCiphertext) || !bytes.Equal(actualNonce, bNonce) || actualVersion != bVersion {
		t.Fatal("stale confirmation changed replacement enrollment identity")
	}
	var confirmedAudits int
	if err = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE actor_user_id=$1 AND event_type='TOTP_ENROLLMENT_CONFIRMED'`, user.id).Scan(&confirmedAudits); err != nil {
		t.Fatal(err)
	}
	if confirmedAudits != 0 {
		t.Fatalf("stale confirmation emitted %d confirmed audit events", confirmedAudits)
	}

	if err = h.service.ConfirmTOTPEnrollment(context.Background(), actor, h.code(secretB), LoginInput{ClientIP: netip.MustParseAddr("192.0.2.10")}); err != nil {
		t.Fatalf("confirm replacement enrollment: %v", err)
	}
	var confirmedAt *time.Time
	var acceptedStep int64
	if err = h.pool.QueryRow(context.Background(), `SELECT enabled_at,last_used_step FROM user_totp WHERE user_id=$1`, user.id).Scan(&confirmedAt, &acceptedStep); err != nil {
		t.Fatal(err)
	}
	if confirmedAt == nil || acceptedStep != h.clock.Now().Unix()/TOTPPeriodSeconds {
		t.Fatalf("replacement confirmation enabledAt=%v acceptedStep=%d", confirmedAt, acceptedStep)
	}
}

func TestTOTPChallengeIssuanceBurstIsBoundedAndSuccessResets(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "challenge-rate-user", false)
	secret := h.enrollAndConfirm(t, user)
	const requests = 20
	statuses := make([]int, requests)
	tokens := make([]string, requests)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			body := strings.NewReader(`{"username":"` + user.username + `","password":"` + user.password + `","deviceName":"burst-device"}`)
			request := httptest.NewRequest(http.MethodPost, "http://relayshelf.test/api/v1/auth/login", body)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://relayshelf.test")
			recorder := httptest.NewRecorder()
			h.router.ServeHTTP(recorder, request)
			statuses[i] = recorder.Code
			var decoded map[string]any
			_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
			tokens[i], _ = decoded["challengeToken"].(string)
		}(i)
	}
	close(start)
	wg.Wait()
	accepted, limited, token := 0, 0, ""
	for i, status := range statuses {
		switch status {
		case http.StatusAccepted:
			accepted++
			token = tokens[i]
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("challenge issuance statuses=%v", statuses)
		}
	}
	if accepted != 3 || limited != requests-3 || token == "" {
		t.Fatalf("accepted=%d limited=%d statuses=%v", accepted, limited, statuses)
	}
	var rows int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM totp_challenges WHERE user_id=$1`, user.id).Scan(&rows); err != nil || rows != 3 {
		t.Fatalf("challenge rows=%d err=%v", rows, err)
	}
	h.clock.Advance(2 * time.Second)
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": token, "code": h.code(secret)}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("completion after temporary bound status=%d body=%v", response.Code, body)
	}
	response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": user.username, "password": user.password, "deviceName": "after-success"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("challenge after success status=%d", response.Code)
	}
}

func TestConcurrentTOTPChallengesClaimOneStepAndCreateOneDeviceSession(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "race-user", false)
	secret := h.enrollAndConfirm(t, user)

	tokens := make([]string, 2)
	for i := range tokens {
		response, body := h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": user.username, "password": user.password, "deviceName": "pending-race-device"}, nil)
		if response.Code != http.StatusAccepted {
			t.Fatalf("challenge %d status=%d body=%v", i, response.Code, body)
		}
		tokens[i], _ = body["challengeToken"].(string)
	}
	var beforeDevices int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE user_id=$1`, user.id).Scan(&beforeDevices); err != nil {
		t.Fatal(err)
	}
	if beforeDevices != 0 {
		t.Fatalf("password-only challenge persisted %d device(s)", beforeDevices)
	}

	code := h.code(secret)
	start := make(chan struct{})
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			body := strings.NewReader(`{"challengeToken":"` + tokens[i] + `","code":"` + code + `"}`)
			request := httptest.NewRequest(http.MethodPost, "http://relayshelf.test/api/v1/auth/login/totp", body)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://relayshelf.test")
			recorder := httptest.NewRecorder()
			h.router.ServeHTTP(recorder, request)
			statuses[i] = recorder.Code
		}(i)
	}
	close(start)
	wg.Wait()
	successes, rejections := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			successes++
		case http.StatusUnauthorized:
			rejections++
		default:
			t.Fatalf("unexpected concurrent statuses=%v", statuses)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("concurrent statuses=%v successes=%d rejections=%d", statuses, successes, rejections)
	}
	var sessions, devices int
	var lastStep int64
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id=$1`, user.id).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE user_id=$1`, user.id).Scan(&devices); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(context.Background(), `SELECT last_used_step FROM user_totp WHERE user_id=$1`, user.id).Scan(&lastStep); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || devices != 1 || lastStep != h.clock.Now().Unix()/TOTPPeriodSeconds {
		t.Fatalf("sessions=%d devices=%d lastStep=%d", sessions, devices, lastStep)
	}
	var name, userAgent string
	if err := h.pool.QueryRow(context.Background(), `SELECT d.name,d.user_agent FROM sessions s JOIN devices d ON d.id=s.device_id WHERE s.user_id=$1`, user.id).Scan(&name, &userAgent); err != nil {
		t.Fatal(err)
	}
	if name != "pending-race-device" || userAgent != "" {
		t.Fatalf("device name=%q userAgent=%q", name, userAgent)
	}
}

func TestTOTPCompletionReusesOwnedDevice(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "reuse-user", false)
	first, err := h.service.Login(context.Background(), LoginInput{Username: user.username, Password: user.password, DeviceName: "owned-device", UserAgent: "original-agent", ClientIP: netip.MustParseAddr("192.0.2.20")})
	if err != nil {
		t.Fatal(err)
	}
	secret := h.enrollAndConfirm(t, user)
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": user.username, "password": user.password, "deviceId": first.Device.ID.String(), "deviceName": "ignored-name"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("challenge status=%d body=%v", response.Code, body)
	}
	response, body = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": body["challengeToken"], "code": h.code(secret)}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("completion status=%d body=%v", response.Code, body)
	}
	var devices, reusedSessions int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE user_id=$1`, user.id).Scan(&devices); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id=$1 AND device_id=$2`, user.id, first.Device.ID).Scan(&reusedSessions); err != nil {
		t.Fatal(err)
	}
	if devices != 1 || reusedSessions != 2 {
		t.Fatalf("devices=%d sessions on owned device=%d", devices, reusedSessions)
	}
}

func TestTOTPChallengeCleanupIsBoundedAndPreservesRecentRows(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "cleanup-user", false)
	now := h.clock.Now()
	insert := func(id uuid.UUID, marker byte, expires time.Time, consumed *time.Time) {
		t.Helper()
		hash := make([]byte, 32)
		hash[0] = marker
		if _, err := h.pool.Exec(context.Background(), `INSERT INTO totp_challenges(id,user_id,token_hash,expires_at,consumed_at,created_at) VALUES($1,$2,$3,$4,$5,$6)`, id, user.id, hash, expires, consumed, now.Add(-3*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-2 * time.Hour)
	recent := now.Add(-10 * time.Minute)
	for i := byte(1); i <= 3; i++ {
		insert(uuid.Must(uuid.NewV7()), i, old, nil)
	}
	insert(uuid.Must(uuid.NewV7()), 4, now.Add(time.Minute), &old)
	liveID := uuid.Must(uuid.NewV7())
	insert(liveID, 5, now.Add(time.Minute), nil)
	recentID := uuid.Must(uuid.NewV7())
	insert(recentID, 6, now.Add(time.Minute), &recent)

	q := generated.New(h.pool)
	params := generated.DeleteExpiredTOTPChallengesParams{ExpiresAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}, Limit: 2}
	if deleted, err := q.DeleteExpiredTOTPChallenges(context.Background(), params); err != nil || deleted != 2 {
		t.Fatalf("first cleanup deleted=%d err=%v", deleted, err)
	}
	if deleted, err := q.DeleteExpiredTOTPChallenges(context.Background(), params); err != nil || deleted != 2 {
		t.Fatalf("second cleanup deleted=%d err=%v", deleted, err)
	}
	if deleted, err := q.DeleteExpiredTOTPChallenges(context.Background(), params); err != nil || deleted != 0 {
		t.Fatalf("idempotent cleanup deleted=%d err=%v", deleted, err)
	}
	var survivors int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM totp_challenges WHERE id=ANY($1)`, []uuid.UUID{liveID, recentID}).Scan(&survivors); err != nil || survivors != 2 {
		t.Fatalf("survivors=%d err=%v", survivors, err)
	}
}

func TestTOTPChallengeExpiry(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "bob", false)
	h.enrollAndConfirm(t, user)

	response, body := h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "bob", "password": user.password, "deviceName": "d"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("login status=%d", response.Code)
	}
	token, _ := body["challengeToken"].(string)
	h.clock.Advance(TOTPChallengeLifetime + time.Minute)
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": token, "code": "123456"}, nil); response.Code != http.StatusGone {
		t.Fatalf("expired challenge status=%d", response.Code)
	}
}

func TestTOTPRateLimitAndLockout(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "carol", false)
	// The session must exist before enrollment gates password logins.
	headers := h.sessionFor(t, user)
	secret := h.enrollAndConfirm(t, user)

	response, body := h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "carol", "password": user.password, "deviceName": "d"}, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("login status=%d", response.Code)
	}
	token, _ := body["challengeToken"].(string)
	// The shared login limiter engages on repeated failures; both 401 and
	// finally 429 are acceptable outcomes of exhausting the challenge.
	sawUnauthorized := 0
	limited := false
	for i := 0; i < MaxTOTPChallengeAttempts; i++ {
		response, _ = h.call(t, http.MethodPost, "/api/v1/auth/login/totp", map[string]any{"challengeToken": token, "code": "000000"}, nil)
		switch response.Code {
		case http.StatusUnauthorized:
			sawUnauthorized++
		case http.StatusTooManyRequests:
			limited = true
		default:
			t.Fatalf("attempt %d status=%d", i, response.Code)
		}
	}
	if sawUnauthorized == 0 || !limited {
		t.Fatalf("unauthorized=%d limited=%v", sawUnauthorized, limited)
	}

	// Repeated wrong owner-visible codes lock the enrollment for a while.
	h.clock.Advance(time.Minute)
	rejected := 0
	for i := 0; i <= MaxTOTPFailedAttempts+1; i++ {
		response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/disable", map[string]any{"code": "000000"}, headers)
		if response.Code == http.StatusUnauthorized {
			rejected++
			continue
		}
		break
	}
	if rejected == 0 {
		t.Fatal("disable attempts never returned 401")
	}
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/disable", map[string]any{"code": h.code(secret)}, headers); response.Code != http.StatusTooManyRequests {
		t.Fatalf("locked disable status=%d", response.Code)
	}
}

func TestTOTPDisabledUserAndDisableFlow(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "dave", false)
	// Session first: once TOTP is confirmed, password logins are challenged.
	headers := h.sessionFor(t, user)
	secret := h.enrollAndConfirm(t, user)

	// Disabled users cannot even reach the second factor.
	if _, err := h.pool.Exec(context.Background(), `UPDATE users SET status='DISABLED' WHERE id=$1`, user.id); err != nil {
		t.Fatal(err)
	}
	if response, _ := h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "dave", "password": user.password, "deviceName": "d"}, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled login status=%d", response.Code)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE users SET status='ACTIVE' WHERE id=$1`, user.id); err != nil {
		t.Fatal(err)
	}

	if response, _ := h.call(t, http.MethodPost, "/api/v1/auth/totp/disable", map[string]any{"code": "000000"}, headers); response.Code != http.StatusUnauthorized {
		t.Fatalf("disable wrong code status=%d", response.Code)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE user_totp SET locked_until=NULL, failed_attempts=0 WHERE user_id=$1`, user.id); err != nil {
		t.Fatal(err)
	}
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/totp/disable", map[string]any{"code": h.code(secret)}, headers)
	if response.Code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("disable status=%d body=%v", response.Code, body)
	}

	// Password-only login works again and the row is gone.
	if response, _ := h.call(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "dave", "password": user.password, "deviceName": "d"}, nil); response.Code != http.StatusOK {
		t.Fatalf("post-disable login status=%d", response.Code)
	}
	var count int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_totp WHERE user_id=$1`, user.id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("totp rows=%d err=%v", count, err)
	}
}

func TestAdminSecurityGateProjection(t *testing.T) {
	h := newTOTPHarness(t)
	admin := h.createUser(t, "rootadmin", true)
	h.createUser(t, "eve", false)
	headers := h.sessionFor(t, admin)

	withoutTOTP := func() int {
		var count int
		if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users u WHERE u.status='ACTIVE' AND u.is_admin AND NOT EXISTS(SELECT 1 FROM user_totp t WHERE t.user_id=u.id AND t.enabled_at IS NOT NULL)`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if withoutTOTP() != 1 {
		t.Fatalf("expected one admin without totp, got %d", withoutTOTP())
	}

	// Enabling admin TOTP satisfies the gate; the ordinary user is free to
	// stay without TOTP. The HTTP projection is covered by the admin module's
	// own integration tests.
	h.enrollAndConfirm(t, admin)
	if withoutTOTP() != 0 {
		t.Fatalf("admin enabled but without=%d", withoutTOTP())
	}
	var ordinaryWithout int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_totp t JOIN users u ON u.id=t.user_id WHERE u.username='eve'`).Scan(&ordinaryWithout); err != nil || ordinaryWithout != 0 {
		t.Fatalf("ordinary user unexpectedly enrolled: %d", ordinaryWithout)
	}
	_ = headers
}

func TestTOTPAuditContainsNoSecretMaterial(t *testing.T) {
	h := newTOTPHarness(t)
	user := h.createUser(t, "frank", false)

	// Enroll through the real endpoints so both audit events fire.
	headers := h.sessionFor(t, user)
	response, body := h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("enroll status=%d body=%v", response.Code, body)
	}
	secret, _ := body["secret"].(string)
	secretBytes, err := DecodeTOTPSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/confirm", map[string]any{"code": h.code(secretBytes)}, headers); response.Code != http.StatusOK {
		t.Fatalf("confirm status=%d", response.Code)
	}
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/enroll", map[string]any{"currentPassword": user.password}, headers); response.Code != http.StatusConflict {
		t.Fatalf("double enroll status=%d", response.Code)
	}
	// The confirmed code is consumed by replay protection; disabling needs a
	// fresh step.
	h.clock.Advance(31 * time.Second)
	if response, _ = h.call(t, http.MethodPost, "/api/v1/auth/totp/disable", map[string]any{"code": h.code(secretBytes)}, headers); response.Code != http.StatusOK {
		t.Fatalf("disable status=%d", response.Code)
	}

	rows, err := h.pool.Query(context.Background(), `SELECT event_type, metadata::text, COALESCE(ip::text,''), COALESCE(user_agent,'') FROM audit_logs WHERE event_type LIKE 'TOTP%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	events := 0
	for rows.Next() {
		var eventType, metadata, ip, userAgent string
		if err = rows.Scan(&eventType, &metadata, &ip, &userAgent); err != nil {
			t.Fatal(err)
		}
		events++
		for _, forbidden := range []string{secret, h.code(secretBytes), "otpauth", "challengeToken", "password"} {
			if strings.Contains(metadata, forbidden) || strings.Contains(userAgent, forbidden) {
				t.Fatalf("audit event %s leaked %q: metadata=%s ua=%s", eventType, forbidden, metadata, userAgent)
			}
		}
	}
	if events < 2 {
		t.Fatalf("expected enrollment-confirmed and disabled events, got %d", events)
	}
}

type nopCloser struct{ reader *strings.Reader }

func (nopCloser) Close() error                 { return nil }
func (n nopCloser) Read(p []byte) (int, error) { return n.reader.Read(p) }

var _ clock.Clock = (*stepClock)(nil)
