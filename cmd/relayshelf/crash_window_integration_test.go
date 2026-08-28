//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// crashHarness boots the real relayshelf binary as a child process against
// the integration PostgreSQL database, with or without a crash failpoint,
// and drives it over real HTTP.
type crashHarness struct {
	t           *testing.T
	databaseURL string
	storage     string
	staging     string
	binary      string
	env         map[string]string
	appKey      string
	csrfKey     string
}

func newCrashHarness(t *testing.T, db *pgxpool.Pool) *crashHarness {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "relayshelf-crash-test")
	build := exec.Command("go", "build", "-o", binary, "./cmd/relayshelf")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build relayshelf: %v\n%s", err, out)
	}
	// Point the child at the same isolated, already-migrated database this
	// test seeded, derived from the pool's live connection settings.
	config := db.Config().ConnConfig
	databaseURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		url.QueryEscape(config.User), url.QueryEscape(config.Password), config.Host, fmt.Sprint(config.Port), config.Database)
	return &crashHarness{
		t:           t,
		databaseURL: databaseURL,
		storage:     t.TempDir(),
		staging:     t.TempDir(),
		binary:      binary,
		env:         map[string]string{},
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func (h *crashHarness) keyMaterial() (appKey, csrfKey string) {
	h.t.Helper()
	if h.appKey != "" {
		return h.appKey, h.csrfKey
	}
	app := make([]byte, 32)
	csrf := make([]byte, 32)
	if _, err := rand.Read(app); err != nil {
		h.t.Fatal(err)
	}
	if _, err := rand.Read(csrf); err != nil {
		h.t.Fatal(err)
	}
	// One key pair per harness so a restarted child decrypts what the
	// crashed child encrypted.
	h.appKey, h.csrfKey = base64.StdEncoding.EncodeToString(app), base64.StdEncoding.EncodeToString(csrf)
	return h.appKey, h.csrfKey
}

// start launches a server child. The returned process handle is monitored;
// when a failpoint fires the child exits by itself, and the wait function
// reports the exit.
func (h *crashHarness) start(failpoint string) (baseURL string, wait func() error, stop func(), err error) {
	h.t.Helper()
	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		return "", nil, nil, listenErr
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	appKey, csrfKey := h.keyMaterial()
	env := append(os.Environ(),
		"DATABASE_URL="+h.databaseURL,
		"STORAGE_ROOT="+h.storage,
		"STAGING_ROOT="+h.staging,
		"APP_ENCRYPTION_KEY="+appKey,
		"CSRF_SECRET="+csrfKey,
		"PUBLIC_ORIGIN="+baseURL,
		"LISTEN_ADDR=127.0.0.1:"+fmt.Sprint(port),
		"STAGING_MIN_FREE_BYTES=0",
		"STAGING_MIN_FREE_PERCENT=0",
		"UPLOAD_STAGING_MAX_BYTES=1073741824",
	)
	if failpoint != "" {
		env = append(env, "RELAYSHELF_TEST_FAILOUT="+failpoint, "RELAYSHELF_TEST_DESTRUCTIVE=1")
	}
	for key, value := range h.env {
		env = append(env, key+"="+value)
	}
	cmd := exec.Command(h.binary, "serve")
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return "", nil, nil, err
	}
	died := make(chan error, 1)
	go func() { died <- cmd.Wait() }()
	stop = func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-died:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-died
		}
	}
	wait = func() error { return <-died }
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, probeErr := http.Get(baseURL + "/health/live")
		if probeErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			return baseURL, wait, stop, nil
		}
		select {
		case err := <-died:
			return "", nil, nil, fmt.Errorf("server exited during startup: %v; stderr: %s", err, stderr.String())
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	return "", nil, nil, fmt.Errorf("server did not become ready")
}

type crashClient struct {
	baseURL string
	cookie  string
	csrf    string
	http    *http.Client
}

func (h *crashHarness) login(t *testing.T, baseURL, username, password string) *crashClient {
	t.Helper()
	client := &crashClient{baseURL: baseURL, http: &http.Client{}}
	payload, _ := json.Marshal(map[string]any{"username": username, "password": password, "deviceName": "crash-window"})
	request, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/login", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", baseURL)
	response, err := client.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("login status=%d body=%s", response.StatusCode, body)
	}
	for _, cookie := range response.Cookies() {
		client.cookie = cookie.Name + "=" + cookie.Value
	}
	var boot struct {
		CSRFToken string `json:"csrfToken"`
	}
	_ = json.NewDecoder(response.Body).Decode(&boot)
	client.csrf = boot.CSRFToken
	return client
}

func (c *crashClient) do(t *testing.T, method, path string, body io.Reader, contentType string) (*http.Response, []byte) {
	t.Helper()
	request, _ := http.NewRequest(method, c.baseURL+path, body)
	request.Header.Set("Origin", c.baseURL)
	request.Header.Set("Cookie", c.cookie)
	if c.csrf != "" {
		request.Header.Set("X-CSRF-Token", c.csrf)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	return response, payload
}

func (c *crashClient) uploadFile(t *testing.T, name string, payload []byte) string {
	t.Helper()
	createBody, _ := json.Marshal(map[string]any{"originalFilename": name, "expectedSize": len(payload)})
	response, body := c.do(t, http.MethodPost, "/api/v1/uploads", bytes.NewReader(createBody), "application/json")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("upload create status=%d body=%s", response.StatusCode, body)
	}
	var session struct {
		ID        string `json:"id"`
		ChunkSize int64  `json:"chunkSize"`
		PartCount int    `json:"partCount"`
	}
	_ = json.Unmarshal(body, &session)
	for part := 0; part < session.PartCount; part++ {
		start := int64(part) * session.ChunkSize
		end := start + session.ChunkSize
		if end > int64(len(payload)) {
			end = int64(len(payload))
		}
		response, body = c.do(t, http.MethodPut, fmt.Sprintf("/api/v1/uploads/%s/parts/%d", session.ID, part), bytes.NewReader(payload[start:end]), "application/octet-stream")
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("part %d status=%d body=%s", part, response.StatusCode, body)
		}
	}
	return session.ID
}

// complete issues the Complete call and tolerates transport errors: when a
// crash failpoint fires mid-request the connection dies by design, which is
// the outcome the crash-window tests assert through process exit instead of
// the HTTP status.
func (c *crashClient) complete(t *testing.T, uploadID string) (*http.Response, []byte) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/uploads/"+uploadID+"/complete", nil)
	request.Header.Set("Origin", c.baseURL)
	request.Header.Set("Cookie", c.cookie)
	request.Header.Set("X-CSRF-Token", c.csrf)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil
	}
	payload, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	return response, payload
}

func seedCrashUser(t *testing.T, db *pgxpool.Pool, username string) string {
	t.Helper()
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	encoded, err := hasher.Hash(crashPassword)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,$3,false,'ACTIVE')`, userID, username, encoded); err != nil {
		t.Fatal(err)
	}
	return username
}

const crashPassword = "crash-window-pass-1"

// TestCrashWindowAfterRename covers the most dangerous window: the NAS
// rename has committed but the READY database update has not. A restarted
// process must reconcile the physical object to READY and the retried
// Complete must succeed without re-uploading anything.
func TestCrashWindowAfterRename(t *testing.T) {
	db := testutil.NewDatabase(t)
	h := newCrashHarness(t, db)
	user := seedCrashUser(t, db, "crash-rename")

	baseURL, wait, stop, err := h.start("after-rename")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("R"), 9<<20)
	client := h.login(t, baseURL, user, crashPassword)
	uploadID := client.uploadFile(t, "rename-crash.bin", payload)

	// The failpoint fires inside Complete; the child must die abruptly.
	response, body := client.complete(t, uploadID)
	_ = response
	_ = body
	exitErr := wait()
	if exitErr == nil {
		stop()
		t.Fatal("child survived the after-rename failpoint")
	}

	// Restart without a failpoint: startup reconciliation runs, then the
	// client retries the same Complete.
	restartBase, _, restartStop, err := h.start("")
	if err != nil {
		t.Fatal(err)
	}
	defer restartStop()
	restarted := h.login(t, restartBase, user, crashPassword)
	var finalStatus string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, body = restarted.complete(t, uploadID)
		var session struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(body, &session)
		finalStatus = session.Status
		if response.StatusCode == http.StatusOK && finalStatus == "COMPLETED" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if finalStatus != "COMPLETED" {
		t.Fatalf("complete after restart status=%q http=%d", finalStatus, response.StatusCode)
	}

	// The physical object exists exactly once and the row is READY.
	var status string
	if err = db.QueryRow(context.Background(), `SELECT status FROM file_objects WHERE sha256 IS NOT NULL AND size_bytes=$1`, len(payload)).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "READY" {
		t.Fatalf("file object status=%s", status)
	}
	entries, readErr := os.ReadDir(filepath.Join(h.storage, "objects"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one physical object, got %d", len(entries))
	}
}

// TestCrashWindowDuringSHA covers a kill during the staging hash pass: the
// session must not become unrecoverable and the retry completes normally.
func TestCrashWindowDuringSHA(t *testing.T) {
	db := testutil.NewDatabase(t)
	h := newCrashHarness(t, db)
	user := seedCrashUser(t, db, "crash-sha")

	baseURL, wait, stop, err := h.start("during-hash")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("H"), 9<<20)
	client := h.login(t, baseURL, user, crashPassword)
	uploadID := client.uploadFile(t, "sha-crash.bin", payload)

	response, body := client.complete(t, uploadID)
	_ = response
	_ = body
	if exitErr := wait(); exitErr == nil {
		stop()
		t.Fatal("child survived the during-hash failpoint")
	}

	restartBase, _, restartStop, err := h.start("")
	if err != nil {
		t.Fatal(err)
	}
	defer restartStop()
	restarted := h.login(t, restartBase, user, crashPassword)
	deadline := time.Now().Add(30 * time.Second)
	var session struct {
		Status string `json:"status"`
	}
	for time.Now().Before(deadline) {
		response, body = restarted.complete(t, uploadID)
		_ = json.Unmarshal(body, &session)
		if response.StatusCode == http.StatusOK && session.Status == "COMPLETED" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("complete after mid-SHA crash status=%q http=%d", session.Status, response.StatusCode)
}

// TestCrashWindowAfterPending covers a kill right after the PENDING row is
// inserted but before any bytes reach NAS: reconciliation must clear the
// reservation and a retry must finalize the whole object again.
func TestCrashWindowAfterPending(t *testing.T) {
	db := testutil.NewDatabase(t)
	h := newCrashHarness(t, db)
	user := seedCrashUser(t, db, "crash-pending")

	baseURL, wait, stop, err := h.start("after-pending")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("P"), 9<<20)
	client := h.login(t, baseURL, user, crashPassword)
	uploadID := client.uploadFile(t, "pending-crash.bin", payload)

	response, body := client.complete(t, uploadID)
	_ = response
	_ = body
	if exitErr := wait(); exitErr == nil {
		stop()
		t.Fatal("child survived the after-pending failpoint")
	}

	restartBase, _, restartStop, err := h.start("")
	if err != nil {
		t.Fatal(err)
	}
	defer restartStop()
	restarted := h.login(t, restartBase, user, crashPassword)
	deadline := time.Now().Add(30 * time.Second)
	var session struct {
		Status string `json:"status"`
	}
	for time.Now().Before(deadline) {
		response, body = restarted.complete(t, uploadID)
		_ = json.Unmarshal(body, &session)
		if response.StatusCode == http.StatusOK && session.Status == "COMPLETED" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("complete after pending-crash status=%q http=%d", session.Status, response.StatusCode)
}

// TestCrashWindowDeleteReconciliation covers the delete side of restart
// consistency: both DELETING shapes must converge, and a shared FileObject
// with a surviving attachment reference must never lose its physical bytes.
func TestCrashWindowDeleteReconciliation(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	h := newCrashHarness(t, db)
	now := time.Now().UTC()
	alice, bob := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, user := range []struct {
		id   uuid.UUID
		name string
	}{{alice, "del-alice"}, {bob, "del-bob"}} {
		if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,$2,$2,'x','ACTIVE')`, user.id, user.name); err != nil {
			t.Fatal(err)
		}
	}

	// One physical object referenced by two owners' messages.
	fileID := uuid.Must(uuid.NewV7())
	hash := sha256Sum(bytes.Repeat([]byte("D"), 4096))
	if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,4096,'application/octet-stream','filesystem',$3,'READY',$4,$4,$4)`, fileID, hash, "objects/"+fileID.String(), now); err != nil {
		t.Fatal(err)
	}
	if err := writeCrashObject(h.storage, fileID, bytes.Repeat([]byte("D"), 4096)); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []uuid.UUID{alice, bob} {
		messageID := uuid.Must(uuid.NewV7())
		if _, err := db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageID, owner, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'shared.bin',0)`, uuid.Must(uuid.NewV7()), messageID, fileID); err != nil {
			t.Fatal(err)
		}
	}

	// A second object stuck in DELETING with its physical file still present,
	// and a third stuck in DELETING with the physical file already gone.
	orphanPresent := uuid.Must(uuid.NewV7())
	orphanAbsent := uuid.Must(uuid.NewV7())
	for index, orphan := range []struct {
		id     uuid.UUID
		exists bool
	}{{orphanPresent, true}, {orphanAbsent, false}} {
		payload := []byte(fmt.Sprintf("orphan-payload-%d", index))
		if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at) VALUES($1,$2,$3,'application/octet-stream','filesystem',$4,'DELETING',$5,$5)`, orphan.id, sha256Sum(payload), len(payload), "objects/"+orphan.id.String(), now); err != nil {
			t.Fatal(err)
		}
		if orphan.exists {
			if err := writeCrashObject(h.storage, orphan.id, payload); err != nil {
				t.Fatal(err)
			}
		}
	}

	// A PENDING object whose final file exists: the rename committed but the
	// READY update never did — restart must promote it.
	pendingID := uuid.Must(uuid.NewV7())
	pendingPayload := []byte("pending-window-bytes")
	if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at) VALUES($1,$2,$3,'application/octet-stream','filesystem',$4,'PENDING',$5,$5)`, pendingID, sha256Sum(pendingPayload), len(pendingPayload), "objects/"+pendingID.String(), now); err != nil {
		t.Fatal(err)
	}
	if err := writeCrashObject(h.storage, pendingID, pendingPayload); err != nil {
		t.Fatal(err)
	}

	_, _, stop, err := h.start("")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var sharedStatus string
		if err = db.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1`, fileID).Scan(&sharedStatus); err != nil {
			t.Fatal(err)
		}
		if sharedStatus != "READY" {
			t.Fatalf("shared object drifted to %s; a referenced FileObject must never be deleted", sharedStatus)
		}
		var pendingGone, deletingGone int
		_ = db.QueryRow(ctx, `SELECT count(*) FROM file_objects WHERE id = ANY($1) AND status='PENDING'`, []uuid.UUID{pendingID}).Scan(&pendingGone)
		_ = db.QueryRow(ctx, `SELECT count(*) FROM file_objects WHERE id = ANY($1) AND status='DELETING'`, []uuid.UUID{orphanPresent, orphanAbsent}).Scan(&deletingGone)
		if pendingGone == 0 && deletingGone == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// The shared object's bytes survived; the orphans are physically gone.
	if _, err := os.Stat(filepath.Join(h.storage, "objects", fileID.String())); err != nil {
		t.Fatalf("shared object bytes lost: %v", err)
	}
	for _, orphan := range []uuid.UUID{orphanPresent, orphanAbsent} {
		if _, err := os.Stat(filepath.Join(h.storage, "objects", orphan.String())); !os.IsNotExist(err) {
			t.Fatalf("orphan %s still present on disk (err=%v)", orphan, err)
		}
	}
	var pendingStatus string
	if err = db.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1`, pendingID).Scan(&pendingStatus); err != nil || pendingStatus != "READY" {
		t.Fatalf("pending object not promoted: status=%s err=%v", pendingStatus, err)
	}
}

// TestCrashWindowJobRunningRecovery covers a worker killed with a job in
// RUNNING: the restarted worker's stuck-job recovery must move it out of
// RUNNING (here: to a retryable failure for a missing physical file) instead
// of leaving it wedged forever.
func TestCrashWindowJobRunningRecovery(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	h := newCrashHarness(t, db)
	now := time.Now().UTC().Add(-time.Hour)
	jobID := uuid.Must(uuid.NewV7())
	subject := uuid.Must(uuid.NewV7())
	started := now
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,attempts,started_at,next_run_at,created_at,updated_at) VALUES($1,'GENERATE_THUMBNAIL','FILE_OBJECT',$2,'RUNNING',1,$3,$3,$3,$3)`, jobID, subject, started); err != nil {
		t.Fatal(err)
	}

	_, _, stop, err := h.start("")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err = db.QueryRow(ctx, `SELECT status FROM background_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "RUNNING" {
			// Recovered out of the wedged state: either retried and failed
			// again on the missing object (bounded, observable) or completed.
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("job stayed RUNNING across restart; stuck recovery did not run")
}

func writeCrashObject(root string, id uuid.UUID, payload []byte) error {
	dir := filepath.Join(root, ".commit-tmp")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o750); err != nil {
		return err
	}
	temp := filepath.Join(dir, id.String()+".tmp")
	if err := os.WriteFile(temp, payload, 0o640); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(root, "objects", id.String()))
}

func sha256Sum(payload []byte) []byte {
	sum := sha256.Sum256(payload)
	return sum[:]
}
