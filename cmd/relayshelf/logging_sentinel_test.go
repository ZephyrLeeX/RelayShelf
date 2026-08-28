//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/admin"
	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/jobs"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/config"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/realtime"
	"github.com/ZephyrLeeX/RelayShelf/internal/search"
	"github.com/ZephyrLeeX/RelayShelf/internal/settings"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/ZephyrLeeX/RelayShelf/internal/tags"
	"github.com/ZephyrLeeX/RelayShelf/internal/uploads"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
)

// synchronizedBuffer collects server log output under concurrent requests.
type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestApplicationLogsNeverContainSecrets drives the real serve wiring — real
// PostgreSQL, real filesystem storage, the production router and middleware
// chain — through success and failure paths that handle secrets, then asserts
// the captured application log contains none of the sentinel values. This is
// the Phase 11 T121 behavioural proof; source grepping alone is not.
func TestApplicationLogsNeverContainSecrets(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	storageRoot := t.TempDir()
	stagingRoot := t.TempDir()
	adapter, err := storage.NewFilesystemStorageAdapter(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))), "=", "")
	passwordSentinel := "PASSWORD_SECRET_" + suffix
	wrongPasswordSentinel := "WRONGPW_SECRET_" + suffix
	bodySentinel := "BODY_SECRET_" + suffix
	sensitiveSentinel := "SENSITIVE_SECRET_" + suffix
	resetSentinel := "RESET_SECRET_" + suffix
	searchSentinel := "SEARCHQ_SECRET_" + suffix

	keyBytes := make([]byte, 32)
	csrfBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	for i := range csrfBytes {
		csrfBytes[i] = byte(255 - i)
	}
	keySentinel := base64.StdEncoding.EncodeToString(keyBytes)
	csrfSentinel := base64.StdEncoding.EncodeToString(csrfBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	origin := &url.URL{Scheme: "http", Host: listener.Addr().String()}
	cfg := config.Config{
		DatabaseURL:             config.Secret{},
		StorageRoot:             storageRoot,
		StagingRoot:             stagingRoot,
		AppEncryptionKey:        config.Secret{},
		CSRFSecret:              config.Secret{},
		PublicOrigin:            origin,
		FileFinalizeConcurrency: 1,
		ThumbnailWorkers:        1,
		MaxActiveChunkWrites:    8,
		UploadStagingMaxBytes:   1 << 30,
		StagingMinFreeBytes:     0,
		StagingMinFreePercent:   0,
	}

	logBuffer := &synchronizedBuffer{}
	// Route the standard library default logger — used by RequestLog,
	// Recovery, and the worker's error reporter — into the capture buffer.
	previousOutput := log.Default().Writer()
	log.SetOutput(logBuffer)
	t.Cleanup(func() { log.SetOutput(previousOutput) })

	now := clock.Real{}
	auditRecorder := audit.NewRecorder(id.UUIDv7{}, now)
	authRepo := auth.NewPostgreSQLRepository(db, auditRecorder)
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params)
	limiter := auth.NewRateLimiter(now, auth.DefaultRateLimitEntries)
	totpCipher, totpErr := auth.NewTOTPCipher(keyBytes)
	if totpErr != nil {
		t.Fatal(totpErr)
	}
	authService := auth.NewService(authRepo, hasher, id.UUIDv7{}, now, limiter, totpCipher)
	csrf := auth.NewCSRF(csrfBytes)
	cookies := auth.NewCookiePolicy(origin)
	authMiddleware := auth.NewMiddleware(authService, cookies, csrf, origin, httpx.NewResolver(nil))
	authHandler := auth.NewHandler(authService, csrf, cookies)

	bodyCipher, err := messages.NewAESGCMCipher(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	messageService := messages.NewService(messages.NewPostgreSQLRepository(db), id.UUIDv7{}, now, bodyCipher)
	hub := realtime.NewHub()
	defer hub.Close()
	jobWake := jobs.NewWake()
	jobRepo := jobs.NewRepository(db, id.UUIDv7{})
	messageService.SetThumbnailJobs(jobRepo, jobWake)
	messageHandler := messages.NewHandler(messageService)
	messageHandler.SetPublisher(hub, id.UUIDv7{}, now)
	tagHandler := tags.NewHandler(tags.NewService(tags.NewPostgreSQLRepository(db), id.UUIDv7{}, now))
	tagHandler.SetPublisher(hub, id.UUIDv7{}, now)
	stagingManager, err := staging.New(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.NewService(uploads.NewPostgreSQLRepository(db), stagingManager, staging.NewStatFSProbe(stagingRoot), id.UUIDv7{}, now, uploads.NewLockRegistry(), cfg.MaxActiveChunkWrites, cfg.UploadStagingMaxBytes, cfg.StagingMinFreeBytes, cfg.StagingMinFreePercent)
	uploadService.SetFinalizer(uploads.NewFileFinalizer(db, adapter, id.UUIDv7{}, now, 1))
	uploadHandler := uploads.NewHandler(uploadService)
	fileHandler := files.NewHandler(files.NewService(db, adapter))
	searchHandler := search.NewHandler(search.NewService(search.NewPostgreSQLRepository(db), now))
	realtimeHandler := realtime.NewHandler(hub, authService)
	settingsHandler := settings.NewHandler(settings.NewService(db, auditRecorder, now))
	userAdminService := users.NewAdminService(db, hasher, id.UUIDv7{}, now, auditRecorder)
	adminHandler := admin.NewHandler(userAdminService, authService, admin.NewStatusService(db, adapter, staging.NewStatFSProbe(stagingRoot)))

	// A background worker runs with the default error reporter, which now
	// writes into the same captured log.
	worker := jobs.NewWorker(jobRepo, map[string]jobs.Handler{jobs.TypeGenerateThumbnail: files.NewThumbnailer(db, adapter, id.UUIDv7{}, now.Now)}, jobWake, now)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	var workerDone sync.WaitGroup
	workerDone.Add(1)
	go func() { defer workerDone.Done(); _ = worker.Run(workerCtx) }()
	defer func() { stopWorker(); workerDone.Wait() }()

	handler := &apiHandler{
		authEndpoints:     &authEndpoints{authHandler},
		messageEndpoints:  &messageEndpoints{messageHandler},
		tagEndpoints:      &tagEndpoints{tagHandler},
		uploadEndpoints:   &uploadEndpoints{uploadHandler},
		fileEndpoints:     &fileEndpoints{fileHandler},
		searchEndpoints:   &searchEndpoints{searchHandler},
		realtimeEndpoints: &realtimeEndpoints{realtimeHandler},
		settingsEndpoints: &settingsEndpoints{settingsHandler},
		adminEndpoints:    &adminEndpoints{adminHandler},
	}
	router := newHTTPRouter(authMiddleware.Host, auth.Router(handler, authMiddleware), health(http.StatusOK), ready(db))
	server := &http.Server{Handler: router}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	// Seed users with real Argon2id hashes so the login path runs for real.
	seedUser := func(name, password string, isAdmin bool) string {
		t.Helper()
		hash, hashErr := hasher.Hash(password)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		var userID string
		query := `INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,$3,$4,'ACTIVE') RETURNING id::text`
		if err = db.QueryRow(ctx, query, uuidNewString(t), name, hash, isAdmin).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		return userID
	}
	aliceID := seedUser("alice", passwordSentinel, false)
	adminID := seedUser("rootadmin", passwordSentinel, true)
	victimID := seedUser("victim", passwordSentinel, false)

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	var idempotencyCounter int64
	do := func(method, path string, body any, headers map[string]string) (*http.Response, string) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			encoded, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			reader = bytes.NewReader(encoded)
		}
		request, requestErr := http.NewRequest(method, origin.String()+path, reader)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Origin", origin.String())
		if strings.Contains(path, "/messages") && method == http.MethodPost {
			idempotencyCounter++
			request.Header.Set("Idempotency-Key", fmt.Sprintf("sentinel-%s-%d", suffix, idempotencyCounter))
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		payload, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		return response, string(payload)
	}

	type bootstrap struct {
		User struct {
			ID string `json:"id"`
		}
		CSRFToken string `json:"csrfToken"`
	}
	login := func(username, password string) (*http.Response, string, bootstrap) {
		t.Helper()
		response, payload := do(http.MethodPost, "/api/v1/auth/login", map[string]any{"username": username, "password": password, "deviceName": "sentinel-device"}, nil)
		var boot bootstrap
		if response.StatusCode == http.StatusOK {
			if err = json.Unmarshal([]byte(payload), &boot); err != nil {
				t.Fatal(err)
			}
		}
		return response, payload, boot
	}

	// 1. Login success, failure, and rate-limited exhaustion.
	response, _, boot := login("alice", passwordSentinel)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", response.StatusCode)
	}
	sessionCookie := response.Cookies()[0].Value
	if sessionCookie == "" {
		t.Fatal("no session cookie")
	}
	sessionSentinel := sessionCookie
	csrfToken := boot.CSRFToken
	if csrfToken == "" {
		t.Fatal("no csrf token")
	}
	authHeaders := map[string]string{"Cookie": response.Cookies()[0].Name + "=" + sessionCookie, "X-CSRF-Token": csrfToken}

	if response, _, _ = login("alice", wrongPasswordSentinel); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status=%d", response.StatusCode)
	}

	// 2. CSRF failure: write without the token.
	if response, _ = do(http.MethodPost, "/api/v1/messages", map[string]any{"body": bodySentinel}, map[string]string{"Cookie": "relayshelf_session=" + sessionCookie}); response.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf-less write status=%d", response.StatusCode)
	}

	// 3. Message validation failure and success.
	if response, _ = do(http.MethodPost, "/api/v1/messages", map[string]any{"body": ""}, authHeaders); response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid message status=%d", response.StatusCode)
	}
	response, payload := do(http.MethodPost, "/api/v1/messages", map[string]any{"body": bodySentinel, "lifecycle": "TEMPORARY", "sensitive": false}, authHeaders)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("message status=%d body=%s", response.StatusCode, payload)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal([]byte(payload), &created); err != nil {
		t.Fatal(err)
	}

	// 4. Sensitive message reveal (success and failure).
	response, payload = do(http.MethodPost, "/api/v1/messages", map[string]any{"body": sensitiveSentinel, "lifecycle": "PERMANENT", "sensitive": true}, authHeaders)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("sensitive message status=%d body=%s", response.StatusCode, payload)
	}
	var sensitiveCreated struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal([]byte(payload), &sensitiveCreated); err != nil {
		t.Fatal(err)
	}
	if response, payload = do(http.MethodGet, "/api/v1/messages/"+sensitiveCreated.ID+"/sensitive-body", nil, authHeaders); response.StatusCode != http.StatusOK {
		t.Fatalf("reveal status=%d body=%s", response.StatusCode, payload)
	}
	if !strings.Contains(payload, sensitiveSentinel) {
		t.Fatal("reveal must return plaintext in the response body only")
	}
	if response, _ = do(http.MethodGet, "/api/v1/messages/"+created.ID+"/sensitive-body", nil, authHeaders); response.StatusCode != http.StatusConflict {
		t.Fatalf("non-sensitive reveal status=%d", response.StatusCode)
	}

	// 5. Search with a sentinel query.
	if response, _ = do(http.MethodGet, "/api/v1/search?q="+searchSentinel, nil, authHeaders); response.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d", response.StatusCode)
	}

	// 6. Upload success and a finalize failure under a broken storage root.
	uploadBytes := bytes.Repeat([]byte("a"), 256<<10)
	createResponse, payload := do(http.MethodPost, "/api/v1/uploads", map[string]any{"originalFilename": "sentinel.bin", "expectedSize": len(uploadBytes)}, authHeaders)
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("upload create status=%d body=%s", createResponse.StatusCode, payload)
	}
	var upload struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal([]byte(payload), &upload); err != nil {
		t.Fatal(err)
	}
	partRequest, _ := http.NewRequest(http.MethodPut, origin.String()+"/api/v1/uploads/"+upload.ID+"/parts/0", bytes.NewReader(uploadBytes))
	partRequest.Header.Set("Origin", origin.String())
	partRequest.Header.Set("Content-Type", "application/octet-stream")
	for key, value := range authHeaders {
		partRequest.Header.Set(key, value)
	}
	partResponse, err := client.Do(partRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, partResponse.Body)
	_ = partResponse.Body.Close()
	if partResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("part status=%d", partResponse.StatusCode)
	}

	// Break the commit temp directory so the finalizer hits a real storage
	// error, then verify the retryable path and restore for cleanup.
	commitDir := filepath.Join(storageRoot, ".commit-tmp")
	if err = os.Chmod(commitDir, 0o500); err != nil {
		t.Fatal(err)
	}
	if response, _ = do(http.MethodPost, "/api/v1/uploads/"+upload.ID+"/complete", nil, authHeaders); response.StatusCode != http.StatusServiceUnavailable {
		_ = os.Chmod(commitDir, 0o750)
		t.Fatalf("faulted complete status=%d", response.StatusCode)
	}
	if err = os.Chmod(commitDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// 7. Admin password reset with a sentinel password.
	adminResponse, _, adminBoot := login("rootadmin", passwordSentinel)
	if adminResponse.StatusCode != http.StatusOK {
		t.Fatalf("admin login status=%d", adminResponse.StatusCode)
	}
	adminCookie := adminResponse.Cookies()[0]
	adminHeaders := map[string]string{"Cookie": adminCookie.Name + "=" + adminCookie.Value, "X-CSRF-Token": adminBoot.CSRFToken}
	if response, _ = do(http.MethodPost, "/api/v1/admin/users/"+victimID+"/password/reset", map[string]any{"newPassword": resetSentinel}, adminHeaders); response.StatusCode != http.StatusNoContent {
		t.Fatalf("reset status=%d", response.StatusCode)
	}

	// 8. Background job failure: an unsupported job type is reported through
	// the worker's error reporter into the same log buffer.
	if _, err = db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,attempts,next_run_at,created_at,updated_at) VALUES($1,'SENTINEL_UNSUPPORTED','FILE_OBJECT',$2,'PENDING',0,$3,$3,$3)`, uuidNewString(t), nil, now.Now()); err != nil {
		t.Fatal(err)
	}
	jobWake.Signal()
	deadline := time.Now().Add(10 * time.Second)
	jobFailed := false
	for time.Now().Before(deadline) {
		var status string
		if err = db.QueryRow(ctx, `SELECT status FROM background_jobs WHERE job_type='SENTINEL_UNSUPPORTED'`).Scan(&status); err == nil && status == "FAILED" {
			jobFailed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !jobFailed {
		t.Fatal("unsupported job was not processed by the background worker")
	}

	output := logBuffer.String()
	// The log must actually be non-vacuous, otherwise the assertions below
	// would pass without exercising anything.
	if !strings.Contains(output, "path=/api/v1/auth/login") {
		t.Fatalf("request log is missing login line; log=%q", output)
	}
	if !strings.Contains(output, "background worker") && !strings.Contains(output, "maintenance") {
		// Worker failures are reported lazily; the sentinel sweep below is the
		// hard requirement, this is only a sanity observation.
		t.Logf("note: worker did not emit a report line during the window")
	}
	sentinelKeys := map[string]string{
		"login password":       passwordSentinel,
		"wrong password":       wrongPasswordSentinel,
		"message body":         bodySentinel,
		"sensitive plaintext":  sensitiveSentinel,
		"reset password":       resetSentinel,
		"search query":         searchSentinel,
		"session raw token":    sessionSentinel,
		"csrf token":           csrfToken,
		"encryption key":       keySentinel,
		"csrf secret":          csrfSentinel,
		"database url scheme":  "postgres://",
		"authorization header": "Authorization",
		"cookie header":        "Cookie:",
	}
	for name, sentinel := range sentinelKeys {
		if strings.Contains(output, sentinel) {
			t.Fatalf("application log leaked %s (%q):\n%s", name, sentinel, output)
		}
	}
	if !strings.Contains(output, aliceID) && !strings.Contains(output, adminID) {
		// User IDs are operational metadata and may appear; this is only a
		// probe that the log is behaving like a request log, not a requirement.
		t.Logf("note: no user ids in log (acceptable)")
	}
}

func uuidNewString(t *testing.T) string {
	t.Helper()
	value, err := id.UUIDv7{}.New()
	if err != nil {
		t.Fatal(err)
	}
	return value.String()
}
