//go:build integration

package messages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/jobs"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/realtime"
	"github.com/ZephyrLeeX/RelayShelf/internal/tags"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type fixture struct {
	db                                           *pgxpool.Pool
	service                                      *messages.Service
	tags                                         *tags.Service
	clock                                        *fixedClock
	jobRepo                                      *jobs.Repository
	wake                                         *jobs.Wake
	alice, bob, disabled, aliceDevice, bobDevice uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := postgresutil.NewDatabase(t)
	ctx := context.Background()
	f := &fixture{db: db, clock: &fixedClock{now: time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)}, alice: uuid.Must(uuid.NewV7()), bob: uuid.Must(uuid.NewV7()), disabled: uuid.Must(uuid.NewV7()), aliceDevice: uuid.Must(uuid.NewV7()), bobDevice: uuid.Must(uuid.NewV7())}
	for _, u := range []struct {
		id           uuid.UUID
		name, status string
	}{{f.alice, "alice", "ACTIVE"}, {f.bob, "bob", "ACTIVE"}, {f.disabled, "disabled", "DISABLED"}} {
		if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status)VALUES($1,$2,$2,'unused',$3)`, u.id, u.name, u.status); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []struct{ id, user uuid.UUID }{{f.aliceDevice, f.alice}, {f.bobDevice, f.bob}} {
		if _, err := db.Exec(ctx, `INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at)VALUES($1,$2,'test','test',$3,$3)`, d.id, d.user, f.clock.now); err != nil {
			t.Fatal(err)
		}
	}
	cipher, err := messages.NewAESGCMCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	f.service = messages.NewService(messages.NewPostgreSQLRepository(db), id.UUIDv7{}, f.clock, cipher)
	f.jobRepo, f.wake = jobs.NewRepository(db, id.UUIDv7{}), jobs.NewWake()
	f.service.SetThumbnailJobs(f.jobRepo, f.wake)
	f.tags = tags.NewService(tags.NewPostgreSQLRepository(db), id.UUIDv7{}, f.clock)
	return f
}
func (f *fixture) create(t *testing.T, owner, device uuid.UUID, key, body, lifecycle string, sensitive bool, tagIDs ...uuid.UUID) messages.Message {
	t.Helper()
	m, err := f.service.Create(context.Background(), owner, device, messages.CreateCommand{Body: body, BodyFormat: messages.Text, Lifecycle: lifecycle, Sensitive: sensitive, TagIDs: tagIDs, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fixture) authenticatedRequest(method, path string, body []byte) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request.WithContext(auth.ContextWithAuthentication(request.Context(), auth.Authentication{User: auth.User{ID: f.alice}, Device: auth.Device{ID: f.aliceDevice}}))
}

func (f *fixture) completedUpload(t *testing.T, owner uuid.UUID, name string, content byte) uuid.UUID {
	return f.completedUploadMIME(t, owner, name, content, "application/octet-stream")
}
func (f *fixture) completedUploadMIME(t *testing.T, owner uuid.UUID, name string, content byte, mime string) uuid.UUID {
	t.Helper()
	fileID, uploadID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	hash := bytes.Repeat([]byte{content}, 32)
	if _, err := f.db.Exec(context.Background(), `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,1,$3,'filesystem',$4,'READY',$5,$5,$5)`, fileID, hash, mime, "objects/"+fileID.String(), f.clock.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(context.Background(), `INSERT INTO upload_sessions(id,user_id,original_filename,expected_size,chunk_size,status,file_object_id,expires_at,completed_at,created_at,updated_at) VALUES($1,$2,$3,1,8388608,'COMPLETED',$4,$5,$6,$6,$6)`, uploadID, owner, name, fileID, f.clock.now.Add(time.Hour), f.clock.now); err != nil {
		t.Fatal(err)
	}
	return uploadID
}

type failingThumbnailEnsurer struct{}

func (failingThumbnailEnsurer) EnsureThumbnailJobTx(context.Context, pgx.Tx, uuid.UUID, string, time.Time) (bool, error) {
	return false, errors.New("injected job insert failure")
}

type eventRecorder struct {
	mu     sync.Mutex
	events map[uuid.UUID][]realtime.Event
}

func (p *eventRecorder) Publish(user uuid.UUID, event realtime.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.events == nil {
		p.events = map[uuid.UUID][]realtime.Event{}
	}
	p.events[user] = append(p.events[user], event)
}
func (p *eventRecorder) count(user uuid.UUID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events[user])
}

func TestPhase7CommitBeforePublishHTTPPaths(t *testing.T) {
	f := newFixture(t)
	publisher := &eventRecorder{}
	handler := messages.NewHandler(f.service)
	handler.SetPublisher(publisher, id.UUIDv7{}, f.clock)
	upload := f.completedUploadMIME(t, f.alice, "event.png", 0x71, "image/png")
	f.service.SetThumbnailJobs(failingThumbnailEnsurer{}, f.wake)
	body := fmt.Sprintf(`{"bodyFormat":"TEXT","lifecycle":"TEMPORARY","uploadIds":[%q]}`, upload)
	request := f.authenticatedRequest(http.MethodPost, "/api/v1/messages", []byte(body))
	w := httptest.NewRecorder()
	handler.CreateMessage(w, request, httpapi.CreateMessageParams{IdempotencyKey: "event-rollback"})
	if w.Code != http.StatusInternalServerError || publisher.count(f.alice) != 0 {
		t.Fatalf("rollback status=%d events=%d", w.Code, publisher.count(f.alice))
	}
	f.service.SetThumbnailJobs(f.jobRepo, f.wake)
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages", []byte(body))
	w = httptest.NewRecorder()
	handler.CreateMessage(w, request, httpapi.CreateMessageParams{IdempotencyKey: "event-success"})
	if w.Code != http.StatusCreated || publisher.count(f.alice) != 1 {
		t.Fatalf("create status=%d events=%d body=%s", w.Code, publisher.count(f.alice), w.Body.String())
	}
	var created httpapi.Message
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages", []byte(body))
	w = httptest.NewRecorder()
	handler.CreateMessage(w, request, httpapi.CreateMessageParams{IdempotencyKey: "event-success"})
	if w.Code != http.StatusCreated || publisher.count(f.alice) != 1 {
		t.Fatalf("create replay status=%d events=%d", w.Code, publisher.count(f.alice))
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages/"+created.Id.String()+"/make-permanent", []byte(`{"expectedVersion":1}`))
	w = httptest.NewRecorder()
	handler.MakeMessagePermanent(w, request, httpapi.MessageId(created.Id))
	if w.Code != http.StatusOK || publisher.count(f.alice) != 2 {
		t.Fatalf("edit status=%d events=%d", w.Code, publisher.count(f.alice))
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages/"+created.Id.String()+"/make-permanent", []byte(`{"expectedVersion":2}`))
	w = httptest.NewRecorder()
	handler.MakeMessagePermanent(w, request, httpapi.MessageId(created.Id))
	if w.Code != http.StatusOK || publisher.count(f.alice) != 2 {
		t.Fatalf("no-op status=%d events=%d", w.Code, publisher.count(f.alice))
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages/direct-send", []byte(fmt.Sprintf(`{"recipientUserId":%q,"body":"direct","bodyFormat":"TEXT"}`, f.bob)))
	w = httptest.NewRecorder()
	handler.DirectSendMessage(w, request, httpapi.DirectSendMessageParams{IdempotencyKey: "event-direct"})
	if w.Code != http.StatusCreated || publisher.count(f.bob) != 1 {
		t.Fatalf("direct status=%d receiver events=%d", w.Code, publisher.count(f.bob))
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages/direct-send", []byte(fmt.Sprintf(`{"recipientUserId":%q,"body":"direct","bodyFormat":"TEXT"}`, f.bob)))
	w = httptest.NewRecorder()
	handler.DirectSendMessage(w, request, httpapi.DirectSendMessageParams{IdempotencyKey: "event-direct"})
	if w.Code != http.StatusCreated || publisher.count(f.bob) != 1 || publisher.count(f.alice) != 2 {
		t.Fatalf("direct replay status=%d receiver=%d sender=%d", w.Code, publisher.count(f.bob), publisher.count(f.alice))
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages/"+created.Id.String()+"/forward", []byte(fmt.Sprintf(`{"recipientUserId":%q,"expectedVersion":2}`, f.bob)))
	w = httptest.NewRecorder()
	handler.ForwardMessage(w, request, httpapi.MessageId(created.Id), httpapi.ForwardMessageParams{IdempotencyKey: "event-forward"})
	if w.Code != http.StatusCreated || publisher.count(f.bob) != 2 || publisher.count(f.alice) != 2 {
		t.Fatalf("forward status=%d receiver=%d sender=%d", w.Code, publisher.count(f.bob), publisher.count(f.alice))
	}
	request = f.authenticatedRequest(http.MethodPost, "/api/v1/messages/"+created.Id.String()+"/forward", []byte(fmt.Sprintf(`{"recipientUserId":%q,"expectedVersion":2}`, f.bob)))
	w = httptest.NewRecorder()
	handler.ForwardMessage(w, request, httpapi.MessageId(created.Id), httpapi.ForwardMessageParams{IdempotencyKey: "event-forward"})
	if w.Code != http.StatusCreated || publisher.count(f.bob) != 2 {
		t.Fatalf("forward replay status=%d receiver=%d", w.Code, publisher.count(f.bob))
	}
	publisher.mu.Lock()
	aliceEvents := append([]realtime.Event(nil), publisher.events[f.alice]...)
	bobEvents := append([]realtime.Event(nil), publisher.events[f.bob]...)
	publisher.mu.Unlock()
	if aliceEvents[0].Type != realtime.MessageCreated || aliceEvents[0].Version == nil || aliceEvents[1].Type != realtime.MessageUpdated || aliceEvents[1].Version == nil || *aliceEvents[1].Version != 2 {
		t.Fatalf("alice events=%+v", aliceEvents)
	}
	if len(bobEvents) != 2 || bobEvents[0].Type != realtime.MessageCreated || bobEvents[0].Version == nil || bobEvents[0].OriginDeviceID == nil || *bobEvents[0].OriginDeviceID != f.aliceDevice || bobEvents[1].Version == nil {
		t.Fatalf("bob event=%+v", bobEvents[0])
	}
}

func TestPhase7ThumbnailJobAtomicMutationPaths(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	imageUpload := f.completedUploadMIME(t, f.alice, "image.png", 0x61, "image/png")
	created, err := f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: []uuid.UUID{imageUpload}, IdempotencyKey: "phase7-image"})
	if err != nil {
		t.Fatal(err)
	}
	fileID := created.Attachments[0].FileObjectID
	var count int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM background_jobs WHERE job_type='GENERATE_THUMBNAIL' AND subject_id=$1`, fileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("create job count=%d err=%v", count, err)
	}
	forward, err := f.service.Forward(ctx, f.alice, f.aliceDevice, messages.ForwardCommand{SourceID: created.ID, RecipientID: f.bob, ExpectedVersion: created.Version, IdempotencyKey: "phase7-forward"})
	if err != nil {
		t.Fatal(err)
	}
	if forward.MessageID == uuid.Nil {
		t.Fatal("forward missing message")
	}
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM background_jobs WHERE subject_id=$1`, fileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("forward dedup count=%d err=%v", count, err)
	}
	directUpload := f.completedUploadMIME(t, f.alice, "direct.jpg", 0x62, "image/jpeg")
	direct, err := f.service.DirectSend(ctx, f.alice, f.aliceDevice, messages.DirectSendCommand{RecipientID: f.bob, BodyFormat: messages.Text, UploadIDs: []uuid.UUID{directUpload}, IdempotencyKey: "phase7-direct"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM background_jobs j JOIN message_attachments ma ON ma.file_object_id=j.subject_id WHERE ma.message_id=$1 AND j.job_type='GENERATE_THUMBNAIL'`, direct.MessageID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("direct job count=%d err=%v", count, err)
	}
	nonImage := f.completedUploadMIME(t, f.alice, "safe.svg", 0x63, "image/svg+xml")
	if _, err = f.service.AddAttachments(ctx, f.alice, created.ID, created.Version, []uuid.UUID{nonImage}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM background_jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("non-image changed job count=%d err=%v", count, err)
	}
	rollbackUpload := f.completedUploadMIME(t, f.alice, "rollback.png", 0x64, "image/png")
	f.service.SetThumbnailJobs(failingThumbnailEnsurer{}, f.wake)
	if _, err = f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: []uuid.UUID{rollbackUpload}, IdempotencyKey: "phase7-rollback"}); err == nil {
		t.Fatal("injected job failure succeeded")
	}
	var consumed *time.Time
	if err = f.db.QueryRow(ctx, `SELECT consumed_at FROM upload_sessions WHERE id=$1`, rollbackUpload).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("rollback upload consumed=%v err=%v", consumed, err)
	}
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE user_id=$1 AND key='phase7-rollback'`, f.alice).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback idempotency count=%d err=%v", count, err)
	}
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM message_attachments ma JOIN upload_sessions us ON us.file_object_id=ma.file_object_id WHERE us.id=$1`, rollbackUpload).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback attachment count=%d err=%v", count, err)
	}
}

func TestPhase5AttachmentBindingConsumptionForwardAndMutation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	u1 := f.completedUpload(t, f.alice, "one.bin", 0x11)
	fileOnly, err := f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: []uuid.UUID{u1}, IdempotencyKey: "file-only"})
	if err != nil {
		t.Fatal(err)
	}
	if fileOnly.BodyPlaintext != nil || len(fileOnly.Attachments) != 1 {
		t.Fatalf("file-only=%+v", fileOnly)
	}
	replay, err := f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: []uuid.UUID{u1}, IdempotencyKey: "file-only"})
	if err != nil || replay.ID != fileOnly.ID {
		t.Fatalf("replay=%+v %v", replay, err)
	}
	if _, err = f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{Body: "x", BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: []uuid.UUID{u1}, IdempotencyKey: "consume-again"}); !errors.Is(err, messages.ErrUploadAlreadyConsumed) {
		t.Fatalf("double consume=%v", err)
	}
	if _, err = f.service.RemoveAttachment(ctx, f.alice, fileOnly.ID, fileOnly.Attachments[0].ID, fileOnly.Version); !errors.Is(err, messages.ErrContentRequired) {
		t.Fatalf("remove last=%v", err)
	}
	u2 := f.completedUpload(t, f.alice, "two.bin", 0x22)
	updated, err := f.service.AddAttachments(ctx, f.alice, fileOnly.ID, fileOnly.Version, []uuid.UUID{u2})
	if err != nil || updated.Version != fileOnly.Version+1 || len(updated.Attachments) != 2 {
		t.Fatalf("add=%+v %v", updated, err)
	}
	updated, err = f.service.RemoveAttachment(ctx, f.alice, fileOnly.ID, updated.Attachments[0].ID, updated.Version)
	if err != nil || updated.Version != fileOnly.Version+2 || len(updated.Attachments) != 1 {
		t.Fatalf("remove=%+v %v", updated, err)
	}
	receipt, err := f.service.Forward(ctx, f.alice, f.aliceDevice, messages.ForwardCommand{SourceID: fileOnly.ID, RecipientID: f.bob, ExpectedVersion: updated.Version, IdempotencyKey: "forward-file"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := f.service.Detail(ctx, f.bob, receipt.MessageID)
	if err != nil || len(bob.Attachments) != 1 || bob.Attachments[0].ID == updated.Attachments[0].ID || bob.Attachments[0].FileObjectID != updated.Attachments[0].FileObjectID {
		t.Fatalf("forward=%+v %v", bob, err)
	}
	u3 := f.completedUpload(t, f.alice, "race.bin", 0x33)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"race-a", "race-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			<-start
			_, createErr := f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{Body: "race", BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: []uuid.UUID{u3}, IdempotencyKey: key})
			results <- createErr
		}(key)
	}
	close(start)
	wg.Wait()
	close(results)
	success, consumed := 0, 0
	for createErr := range results {
		if createErr == nil {
			success++
		} else if errors.Is(createErr, messages.ErrUploadAlreadyConsumed) {
			consumed++
		} else {
			t.Fatalf("race error=%v", createErr)
		}
	}
	if success != 1 || consumed != 1 {
		t.Fatalf("race success=%d consumed=%d", success, consumed)
	}
	many := []uuid.UUID{}
	for i, b := range []byte{0x41, 0x42, 0x43, 0x44} {
		many = append(many, f.completedUpload(t, f.alice, fmt.Sprintf("many-%d.bin", i), b))
	}
	manyMessage, err := f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{BodyFormat: messages.Text, Lifecycle: messages.Temporary, UploadIDs: many, IdempotencyKey: "many"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.service.List(ctx, f.alice, messages.ListFilter{Limit: 100}, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, summary := range page.Items {
		if summary.ID == manyMessage.ID {
			found = true
			if summary.AttachmentCount != 4 || len(summary.Attachments) != 3 {
				t.Fatalf("summary count=%d previews=%d", summary.AttachmentCount, len(summary.Attachments))
			}
		}
	}
	if !found {
		t.Fatal("many attachment summary missing")
	}
}

func TestMessageCreateOwnerBodyVersionAndSensitiveAtRest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ordinary := f.create(t, f.alice, f.aliceDevice, "ordinary", "   ", messages.Temporary, false)
	if ordinary.Version != 1 || ordinary.ExpiresAt == nil || !ordinary.ExpiresAt.Equal(f.clock.now.Add(72*time.Hour)) {
		t.Fatalf("ordinary=%+v", ordinary)
	}
	if _, err := f.service.Detail(ctx, f.bob, ordinary.ID); !errors.Is(err, messages.ErrNotFound) {
		t.Fatalf("owner isolation: %v", err)
	}
	secret := f.create(t, f.alice, f.aliceDevice, "secret", "private body", messages.Permanent, true)
	var plain *string
	var ciphertext, nonce []byte
	var version *int16
	if err := f.db.QueryRow(ctx, `SELECT body_plaintext,body_ciphertext,body_nonce,body_encryption_version FROM messages WHERE id=$1`, secret.ID).Scan(&plain, &ciphertext, &nonce, &version); err != nil {
		t.Fatal(err)
	}
	if plain != nil || len(ciphertext) == 0 || len(nonce) != 12 || version == nil || *version != messages.EncryptionVersion1 {
		t.Fatal("sensitive plaintext/cipher invariant failed")
	}
	revealed, v, err := f.service.Reveal(ctx, f.alice, secret.ID)
	if err != nil || revealed != "private body" || v != 1 {
		t.Fatalf("reveal=%q %d %v", revealed, v, err)
	}
	detail, err := f.service.Detail(ctx, f.alice, secret.ID)
	if err != nil || detail.BodyPlaintext != nil {
		t.Fatal("ordinary detail leaked sensitive body")
	}
	handler := messages.NewHandler(f.service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+secret.ID.String()+"/sensitive-body", nil)
	request = request.WithContext(auth.ContextWithAuthentication(request.Context(), auth.Authentication{User: auth.User{ID: f.alice}}))
	recorder := httptest.NewRecorder()
	handler.RevealSensitiveBody(recorder, request, secret.ID)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("sensitive response status=%d headers=%v", recorder.Code, recorder.Header())
	}
	exact := string(bytes.Repeat([]byte{'x'}, messages.MaxBodyBytes))
	f.create(t, f.alice, f.aliceDevice, "exact", exact, messages.Permanent, false)
	if _, err = f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{Body: exact + "x", BodyFormat: messages.Text, Lifecycle: messages.Permanent, IdempotencyKey: "large"}); !errors.Is(err, messages.ErrValidation) {
		t.Fatalf("oversize accepted: %v", err)
	}
	if _, err = f.service.Create(ctx, f.alice, f.aliceDevice, messages.CreateCommand{Body: "", BodyFormat: messages.Text, Lifecycle: messages.Permanent, IdempotencyKey: "empty"}); !errors.Is(err, messages.ErrValidation) {
		t.Fatalf("empty accepted: %v", err)
	}
}

func TestTagsListFiltersPaginationAndDeleteVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, err := f.tags.Create(ctx, f.alice, " Alpha ", "#3b82f6")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.tags.Create(ctx, f.alice, "Beta", "#FFFFFF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.tags.Create(ctx, f.alice, "alpha", "#000000"); !errors.Is(err, tags.ErrDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	foreign, err := f.tags.Create(ctx, f.bob, "Alpha", "#000000")
	if err != nil {
		t.Fatal(err)
	}
	renamed := "Renamed"
	a, err = f.tags.Update(ctx, f.alice, a.ID, &renamed, nil)
	if err != nil || a.Name != renamed || a.Color != "#3B82F6" {
		t.Fatalf("partial tag update=%+v %v", a, err)
	}
	if _, err = f.tags.Update(ctx, f.bob, a.ID, &renamed, nil); !errors.Is(err, tags.ErrNotFound) {
		t.Fatalf("foreign tag update=%v", err)
	}
	both := f.create(t, f.alice, f.aliceDevice, "both", "界"+string(bytes.Repeat([]byte{'a'}, messages.MaxPreviewBytes)), messages.Permanent, false, a.ID, b.ID)
	f.create(t, f.alice, f.aliceDevice, "only-a", "a", messages.Permanent, false, a.ID)
	bothTwo := f.create(t, f.alice, f.aliceDevice, "both-two", "two", messages.Permanent, false, a.ID, b.ID)
	if _, err = f.service.ReplaceTags(ctx, f.alice, both.ID, both.Version, []uuid.UUID{foreign.ID}); !errors.Is(err, messages.ErrValidation) {
		t.Fatalf("foreign tag=%v", err)
	}
	filter := messages.ListFilter{TagIDs: []uuid.UUID{a.ID, b.ID}, Limit: 1}
	page, err := f.service.List(ctx, f.alice, filter, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("page=%+v", page)
	}
	cursor, err := messages.DecodeCursor(*page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	filter.Cursor = &cursor
	next, err := f.service.List(ctx, f.alice, filter, false)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatalf("next=%+v %v", next, err)
	}
	all, err := f.service.List(ctx, f.alice, messages.ListFilter{TagIDs: []uuid.UUID{a.ID, b.ID}, Limit: 30}, false)
	if err != nil || len(all.Items) != 2 {
		t.Fatalf("AND filter=%+v %v", all, err)
	}
	foundLong := false
	for _, item := range all.Items {
		if item.ID == both.ID {
			foundLong = item.BodyPreview != nil && len(*item.BodyPreview) <= messages.MaxPreviewBytes && item.BodyTruncated
		}
	}
	if !foundLong || bothTwo.ID == uuid.Nil {
		t.Fatal("UTF-8 preview or AND filter failed")
	}
	before := both.Version
	if err = f.tags.Delete(ctx, f.alice, a.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.Detail(ctx, f.alice, both.ID)
	if err != nil || after.Version != before+1 {
		t.Fatalf("tag delete version=%d err=%v", after.Version, err)
	}
}

func TestLifecycleTrashRestoreDeleteAuditAndTTLSnapshots(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.create(t, f.alice, f.aliceDevice, "ttl", "body", messages.Temporary, false)
	originalExpiry := *m.ExpiresAt
	if _, err := f.db.Exec(ctx, `UPDATE system_settings SET temporary_ttl_hours=24,trash_ttl_hours=48 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	permanent, err := f.service.MakePermanent(ctx, f.alice, m.ID, m.Version)
	if err != nil || permanent.ExpiresAt != nil || permanent.Version != 2 {
		t.Fatalf("permanent=%+v %v", permanent, err)
	}
	if _, err = f.service.SetFavorite(ctx, f.alice, m.ID, permanent.Version, true); err != nil {
		t.Fatal(err)
	}
	temporary := f.create(t, f.alice, f.aliceDevice, "ttl2", "body", messages.Temporary, false)
	if !temporary.ExpiresAt.Equal(f.clock.now.Add(24*time.Hour)) || originalExpiry.Equal(*temporary.ExpiresAt) {
		t.Fatal("temporary TTL was not snapshotted")
	}
	trashed, err := f.service.Trash(ctx, f.alice, temporary.ID, temporary.Version)
	if err != nil || trashed.PurgeAt == nil || !trashed.PurgeAt.Equal(f.clock.now.Add(48*time.Hour)) {
		t.Fatalf("trash=%+v %v", trashed, err)
	}
	if _, err = f.db.Exec(ctx, `UPDATE system_settings SET temporary_ttl_hours=12,trash_ttl_hours=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	f.clock.now = f.clock.now.Add(25 * time.Hour)
	restored, err := f.service.Restore(ctx, f.alice, temporary.ID, trashed.Version)
	if err != nil || restored.ExpiresAt == nil || !restored.ExpiresAt.Equal(f.clock.now.Add(12*time.Hour)) {
		t.Fatalf("restore=%+v %v", restored, err)
	}
	if err = f.service.PermanentlyDelete(ctx, f.alice, restored.ID, messages.AuditContext{DeviceID: f.aliceDevice}); !errors.Is(err, messages.ErrNotTrashed) {
		t.Fatalf("active delete=%v", err)
	}
	trashed, err = f.service.Trash(ctx, f.alice, restored.ID, restored.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.PermanentlyDelete(ctx, f.alice, trashed.ID, messages.AuditContext{DeviceID: f.aliceDevice, TraceID: "trace"}); err != nil {
		t.Fatal(err)
	}
	var audit int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE event_type='MESSAGE_PERMANENTLY_DELETED' AND target_id=$1 AND metadata ? 'lifecycle'`, trashed.ID).Scan(&audit); err != nil || audit != 1 {
		t.Fatalf("audit=%d %v", audit, err)
	}
}

func TestExtendTemporaryExpiryContract(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	temporary := f.create(t, f.alice, f.aliceDevice, "extend", "body", messages.Temporary, false)
	originalExpiry := *temporary.ExpiresAt
	f.clock.now = f.clock.now.Add(2 * time.Hour)

	current := temporary
	for _, days := range []int{1, 3, 7} {
		previousExpiry := *current.ExpiresAt
		previousVersion := current.Version
		var err error
		current, err = f.service.ExtendExpiry(ctx, f.alice, current.ID, current.Version, days)
		if err != nil {
			t.Fatalf("extend %d days: %v", days, err)
		}
		if current.ExpiresAt == nil || !current.ExpiresAt.Equal(previousExpiry.Add(time.Duration(days)*24*time.Hour)) || current.Version != previousVersion+1 || !current.UpdatedAt.Equal(f.clock.now) {
			t.Fatalf("extend %d days result=%+v", days, current)
		}
	}
	if want := originalExpiry.Add(11 * 24 * time.Hour); current.ExpiresAt == nil || !current.ExpiresAt.Equal(want) {
		t.Fatalf("expiry=%v want=%v", current.ExpiresAt, want)
	}
	if _, err := f.service.ExtendExpiry(ctx, f.alice, current.ID, current.Version-1, 1); !errors.Is(err, messages.ErrVersionConflict) {
		t.Fatalf("stale version=%v", err)
	}
	if _, err := f.service.ExtendExpiry(ctx, f.bob, current.ID, current.Version, 1); !errors.Is(err, messages.ErrNotFound) {
		t.Fatalf("cross owner=%v", err)
	}
	if _, err := f.service.ExtendExpiry(ctx, f.alice, current.ID, current.Version, 2); !errors.Is(err, messages.ErrValidation) {
		t.Fatalf("invalid days=%v", err)
	}

	permanent := f.create(t, f.alice, f.aliceDevice, "extend-permanent", "body", messages.Permanent, false)
	if _, err := f.service.ExtendExpiry(ctx, f.alice, permanent.ID, permanent.Version, 1); !errors.Is(err, messages.ErrNotTemporary) {
		t.Fatalf("permanent=%v", err)
	}
	trashed, err := f.service.Trash(ctx, f.alice, current.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ExtendExpiry(ctx, f.alice, trashed.ID, trashed.Version, 1); !errors.Is(err, messages.ErrTrashed) {
		t.Fatalf("trashed=%v", err)
	}
}

func TestExtendTemporaryExpiryHTTPPublishesCommittedVersion(t *testing.T) {
	f := newFixture(t)
	message := f.create(t, f.alice, f.aliceDevice, "extend-http", "body", messages.Temporary, false)
	publisher := &eventRecorder{}
	handler := messages.NewHandler(f.service)
	handler.SetPublisher(publisher, id.UUIDv7{}, f.clock)
	payload := []byte(`{"expectedVersion":1,"days":3}`)
	recorder := httptest.NewRecorder()
	handler.ExtendMessageExpiry(recorder, f.authenticatedRequest(http.MethodPost, "/api/v1/messages/"+message.ID.String()+"/extend", payload), httpapi.MessageId(message.ID))
	if recorder.Code != http.StatusOK || publisher.count(f.alice) != 1 {
		t.Fatalf("extend status=%d events=%d body=%s", recorder.Code, publisher.count(f.alice), recorder.Body.String())
	}
	publisher.mu.Lock()
	event := publisher.events[f.alice][0]
	publisher.mu.Unlock()
	if event.Type != realtime.MessageUpdated || event.Version == nil || *event.Version != 2 || event.OriginDeviceID == nil || *event.OriginDeviceID != f.aliceDevice {
		t.Fatalf("event=%+v", event)
	}
	recorder = httptest.NewRecorder()
	handler.ExtendMessageExpiry(recorder, f.authenticatedRequest(http.MethodPost, "/api/v1/messages/"+message.ID.String()+"/extend", payload), httpapi.MessageId(message.ID))
	if recorder.Code != http.StatusConflict || publisher.count(f.alice) != 1 {
		t.Fatalf("stale status=%d events=%d", recorder.Code, publisher.count(f.alice))
	}
}

func TestDirectSendForwardSensitiveAndIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	direct := messages.DirectSendCommand{RecipientID: f.bob, Body: "sent secret", BodyFormat: messages.Markdown, Sensitive: true, IdempotencyKey: "direct"}
	first, err := f.service.DirectSend(ctx, f.alice, f.aliceDevice, direct)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.service.DirectSend(ctx, f.alice, f.aliceDevice, direct)
	if err != nil || again.MessageID != first.MessageID || !again.CreatedAt.Equal(first.CreatedAt) || !again.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("replay=%+v %v", again, err)
	}
	direct.Body = "changed"
	if _, err = f.service.DirectSend(ctx, f.alice, f.aliceDevice, direct); !errors.Is(err, messages.ErrIdempotencyKeyReused) {
		t.Fatalf("reuse=%v", err)
	}
	var receiverOwner, provenance uuid.UUID
	var sourceMessage *uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT owner_id,source_user_id,source_message_id FROM messages WHERE id=$1`, first.MessageID).Scan(&receiverOwner, &provenance, &sourceMessage); err != nil {
		t.Fatal(err)
	}
	if receiverOwner != f.bob || provenance != f.alice || sourceMessage != nil {
		t.Fatal("direct-send ownership or provenance mismatch")
	}
	var metadata string
	if err = f.db.QueryRow(ctx, `SELECT response_metadata::text FROM idempotency_keys WHERE user_id=$1 AND operation='message.direct-send' AND key='direct'`, f.alice).Scan(&metadata); err != nil || strings.Contains(metadata, "sent secret") {
		t.Fatalf("unsafe idempotency metadata=%q %v", metadata, err)
	}
	if strings.Contains(metadata, "body") || strings.Contains(metadata, "tag") || strings.Contains(metadata, "ciphertext") || strings.Contains(metadata, "nonce") {
		t.Fatalf("delivery metadata contains receiver state: %q", metadata)
	}
	received, err := f.service.Detail(ctx, f.bob, first.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	updatedBody := "receiver changed secret"
	received, err = f.service.EditSensitive(ctx, f.bob, received.ID, received.Version, updatedBody)
	if err != nil {
		t.Fatal(err)
	}
	received, err = f.service.MakePermanent(ctx, f.bob, received.ID, received.Version)
	if err != nil {
		t.Fatal(err)
	}
	received, err = f.service.SetFavorite(ctx, f.bob, received.ID, received.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	again, err = f.service.DirectSend(ctx, f.alice, f.aliceDevice, messages.DirectSendCommand{RecipientID: f.bob, Body: "sent secret", BodyFormat: messages.Markdown, Sensitive: true, IdempotencyKey: "direct"})
	if err != nil || again.MessageID != first.MessageID || !again.CreatedAt.Equal(first.CreatedAt) || !again.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("replay after receiver mutation=%+v %v", again, err)
	}
	received, err = f.service.Trash(ctx, f.bob, received.ID, received.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.PermanentlyDelete(ctx, f.bob, received.ID, messages.AuditContext{DeviceID: f.bobDevice}); err != nil {
		t.Fatal(err)
	}
	again, err = f.service.DirectSend(ctx, f.alice, f.aliceDevice, messages.DirectSendCommand{RecipientID: f.bob, Body: "sent secret", BodyFormat: messages.Markdown, Sensitive: true, IdempotencyKey: "direct"})
	if err != nil || again.MessageID != first.MessageID || !again.CreatedAt.Equal(first.CreatedAt) || !again.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("replay after receiver delete=%+v %v", again, err)
	}
	if _, err = f.service.DirectSend(ctx, f.alice, f.aliceDevice, messages.DirectSendCommand{RecipientID: f.disabled, Body: "x", BodyFormat: messages.Text, IdempotencyKey: "disabled"}); !errors.Is(err, messages.ErrRecipientUnavailable) {
		t.Fatalf("disabled=%v", err)
	}
	source := f.create(t, f.alice, f.aliceDevice, "source", "forward secret", messages.Permanent, true)
	forward := messages.ForwardCommand{SourceID: source.ID, RecipientID: f.bob, ExpectedVersion: source.Version, IdempotencyKey: "forward"}
	destination, err := f.service.Forward(ctx, f.alice, f.aliceDevice, forward)
	if err != nil {
		t.Fatal(err)
	}
	destinationMessage, err := f.service.Detail(ctx, f.bob, destination.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := f.service.Reveal(ctx, f.bob, destination.MessageID)
	if err != nil || plain != "forward secret" {
		t.Fatalf("destination reveal=%q %v", plain, err)
	}
	var sourceCipher, destinationCipher []byte
	if err = f.db.QueryRow(ctx, `SELECT body_ciphertext FROM messages WHERE id=$1`, source.ID).Scan(&sourceCipher); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(ctx, `SELECT body_ciphertext FROM messages WHERE id=$1`, destination.MessageID).Scan(&destinationCipher); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sourceCipher, destinationCipher) || destinationMessage.SourceMessageID == nil || *destinationMessage.SourceMessageID != source.ID {
		t.Fatal("forward copied ciphertext or lost provenance")
	}
	replayed, err := f.service.Forward(ctx, f.alice, f.aliceDevice, forward)
	if err != nil || replayed.MessageID != destination.MessageID || !replayed.CreatedAt.Equal(destination.CreatedAt) || !replayed.ExpiresAt.Equal(destination.ExpiresAt) {
		t.Fatalf("forward replay=%+v %v", replayed, err)
	}
	destinationMessage, err = f.service.EditSensitive(ctx, f.bob, destinationMessage.ID, destinationMessage.Version, "receiver changed forward")
	if err != nil {
		t.Fatal(err)
	}
	destinationMessage, err = f.service.MakePermanent(ctx, f.bob, destinationMessage.ID, destinationMessage.Version)
	if err != nil {
		t.Fatal(err)
	}
	destinationMessage, err = f.service.SetFavorite(ctx, f.bob, destinationMessage.ID, destinationMessage.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err = f.service.Forward(ctx, f.alice, f.aliceDevice, forward)
	if err != nil || replayed.MessageID != destination.MessageID || !replayed.CreatedAt.Equal(destination.CreatedAt) || !replayed.ExpiresAt.Equal(destination.ExpiresAt) {
		t.Fatalf("forward replay after receiver mutation=%+v %v", replayed, err)
	}
	destinationMessage, err = f.service.Trash(ctx, f.bob, destinationMessage.ID, destinationMessage.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.PermanentlyDelete(ctx, f.bob, destinationMessage.ID, messages.AuditContext{DeviceID: f.bobDevice}); err != nil {
		t.Fatal(err)
	}
	replayed, err = f.service.Forward(ctx, f.alice, f.aliceDevice, forward)
	if err != nil || replayed.MessageID != destination.MessageID || !replayed.CreatedAt.Equal(destination.CreatedAt) || !replayed.ExpiresAt.Equal(destination.ExpiresAt) {
		t.Fatalf("forward replay after receiver delete=%+v %v", replayed, err)
	}
	stale := forward
	stale.IdempotencyKey = "forward-stale"
	stale.ExpectedVersion++
	if _, err = f.service.Forward(ctx, f.alice, f.aliceDevice, stale); !errors.Is(err, messages.ErrVersionConflict) {
		t.Fatalf("stale forward=%v", err)
	}
	trashed, err := f.service.Trash(ctx, f.alice, source.ID, source.Version)
	if err != nil {
		t.Fatal(err)
	}
	blocked := forward
	blocked.IdempotencyKey = "forward-trash"
	blocked.ExpectedVersion = trashed.Version
	if _, err = f.service.Forward(ctx, f.alice, f.aliceDevice, blocked); !errors.Is(err, messages.ErrTrashed) {
		t.Fatalf("trash forward=%v", err)
	}
}

func TestSensitiveToggleEditTagsFavoriteExpiryAndTrashList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, err := f.tags.Create(ctx, f.alice, "A", "#112233")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.tags.Create(ctx, f.alice, "B", "#445566")
	if err != nil {
		t.Fatal(err)
	}
	m := f.create(t, f.alice, f.aliceDevice, "mutations", "plain", messages.Temporary, false, a.ID)
	if _, err = f.service.SetFavorite(ctx, f.alice, m.ID, m.Version, true); !errors.Is(err, messages.ErrFavoriteRequiresPermanent) {
		t.Fatalf("temporary favorite=%v", err)
	}
	same, err := f.service.ReplaceTags(ctx, f.alice, m.ID, m.Version, []uuid.UUID{a.ID})
	if err != nil || same.Version != m.Version {
		t.Fatalf("same tags bumped version: %+v %v", same, err)
	}
	changed, err := f.service.ReplaceTags(ctx, f.alice, m.ID, m.Version, []uuid.UUID{a.ID, b.ID})
	if err != nil || changed.Version != m.Version+1 {
		t.Fatalf("changed tags=%+v %v", changed, err)
	}
	secret, err := f.service.SetSensitive(ctx, f.alice, m.ID, changed.Version, true)
	if err != nil || !secret.Sensitive {
		t.Fatal(err)
	}
	var oldNonce []byte
	if err = f.db.QueryRow(ctx, `SELECT body_nonce FROM messages WHERE id=$1`, m.ID).Scan(&oldNonce); err != nil {
		t.Fatal(err)
	}
	noOp, err := f.service.SetSensitive(ctx, f.alice, m.ID, secret.Version, true)
	if err != nil || noOp.Version != secret.Version {
		t.Fatalf("same sensitive bumped: %+v %v", noOp, err)
	}
	edited, err := f.service.EditSensitive(ctx, f.alice, m.ID, secret.Version, "new secret")
	if err != nil || edited.Version != secret.Version+1 {
		t.Fatal(err)
	}
	var newNonce []byte
	if err = f.db.QueryRow(ctx, `SELECT body_nonce FROM messages WHERE id=$1`, m.ID).Scan(&newNonce); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldNonce, newNonce) {
		t.Fatal("sensitive edit reused nonce")
	}
	plain, err := f.service.SetSensitive(ctx, f.alice, m.ID, edited.Version, false)
	if err != nil || plain.Sensitive || plain.BodyPlaintext == nil || *plain.BodyPlaintext != "new secret" {
		t.Fatalf("plain=%+v %v", plain, err)
	}
	var cryptoFields int
	if err = f.db.QueryRow(ctx, `SELECT (body_ciphertext IS NOT NULL)::int+(body_nonce IS NOT NULL)::int+(body_encryption_version IS NOT NULL)::int FROM messages WHERE id=$1`, m.ID).Scan(&cryptoFields); err != nil || cryptoFields != 0 {
		t.Fatalf("crypto fields=%d %v", cryptoFields, err)
	}
	if _, err = f.service.MakePermanent(ctx, f.alice, m.ID, plain.Version); err != nil {
		t.Fatal(err)
	}
	due := f.create(t, f.alice, f.aliceDevice, "due", "due", messages.Temporary, false)
	f.create(t, f.alice, f.aliceDevice, "keep", "keep", messages.Permanent, false)
	f.clock.now = f.clock.now.Add(73 * time.Hour)
	active, err := f.service.List(ctx, f.alice, messages.ListFilter{Limit: 100}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range active.Items {
		if item.ID == due.ID {
			t.Fatal("expired temporary remained active")
		}
	}
	affected, err := f.service.ExpireDueTemporary(ctx, 1)
	if err != nil || affected != 1 {
		t.Fatalf("expired=%d %v", affected, err)
	}
	trash, err := f.service.List(ctx, f.alice, messages.ListFilter{Limit: 100}, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range trash.Items {
		if item.ID == due.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expired message missing from trash")
	}
}

func TestReplaceTagsRejectsTrashedMessageWithoutMutation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, err := f.tags.Create(ctx, f.alice, "A", "#112233")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.tags.Create(ctx, f.alice, "B", "#445566")
	if err != nil {
		t.Fatal(err)
	}
	m := f.create(t, f.alice, f.aliceDevice, "trash-tags", "body", messages.Permanent, false, a.ID)
	trashed, err := f.service.Trash(ctx, f.alice, m.ID, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(httpapi.ReplaceMessageTagsRequest{ExpectedVersion: trashed.Version, TagIds: []uuid.UUID{b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	messages.NewHandler(f.service).ReplaceMessageTags(recorder, f.authenticatedRequest(http.MethodPut, "/api/v1/messages/"+m.ID.String()+"/tags", payload), m.ID)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"code":"MESSAGE_TRASHED"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var version int64
	var tagID uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT m.version,mt.tag_id FROM messages m JOIN message_tags mt ON mt.message_id=m.id WHERE m.id=$1`, m.ID).Scan(&version, &tagID); err != nil {
		t.Fatal(err)
	}
	if version != trashed.Version || tagID != a.ID {
		t.Fatalf("trashed tag mutation version=%d tag=%s", version, tagID)
	}
}

func TestHTTPDecodedBodyLimitAllowsEscapedOneMiB(t *testing.T) {
	f := newFixture(t)
	handler := messages.NewHandler(f.service)
	cases := []struct {
		name string
		body string
	}{
		{name: "ascii", body: strings.Repeat("a", messages.MaxBodyBytes)},
		{name: "backslash-heavy", body: strings.Repeat(`\`, messages.MaxBodyBytes)},
		{name: "quote-heavy", body: strings.Repeat(`"`, messages.MaxBodyBytes)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"body": test.body, "lifecycle": messages.Permanent})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			handler.CreateMessage(recorder, f.authenticatedRequest(http.MethodPost, "/api/v1/messages", payload), httpapi.CreateMessageParams{IdempotencyKey: "body-limit-" + test.name})
			if recorder.Code != http.StatusCreated {
				t.Fatalf("encoded=%d status=%d body=%s", len(payload), recorder.Code, recorder.Body.String())
			}
		})
	}
	t.Run("decoded-over-limit", func(t *testing.T) {
		payload, err := json.Marshal(map[string]any{"body": strings.Repeat("a", messages.MaxBodyBytes+1), "lifecycle": messages.Permanent})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		handler.CreateMessage(recorder, f.authenticatedRequest(http.MethodPost, "/api/v1/messages", payload), httpapi.CreateMessageParams{IdempotencyKey: "body-limit-over"})
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestConcurrentVersionAndIdempotencyCreate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.create(t, f.alice, f.aliceDevice, "race-base", "base", messages.Permanent, false)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, body := range []string{"one", "two"} {
		go func(body string) {
			<-start
			_, err := f.service.Edit(ctx, f.alice, m.ID, messages.EditCommand{ExpectedVersion: m.Version, Body: &body})
			errs <- err
		}(body)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-errs
		if err == nil {
			success++
		} else if errors.Is(err, messages.ErrVersionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	command := messages.CreateCommand{Body: "once", BodyFormat: messages.Text, Lifecycle: messages.Permanent, IdempotencyKey: "concurrent"}
	start = make(chan struct{})
	ids := make(chan uuid.UUID, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := f.service.Create(ctx, f.alice, f.aliceDevice, command)
			if err != nil {
				errs <- err
				return
			}
			ids <- created.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	var firstID uuid.UUID
	for value := range ids {
		if firstID == uuid.Nil {
			firstID = value
		} else if value != firstID {
			t.Fatal("idempotency created two resources")
		}
	}
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
	var count int
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE owner_id=$1 AND body_plaintext='once'`, f.alice).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d %v", count, err)
	}
}

func waitForBlockedQuery(t *testing.T, db *pgxpool.Pool, fragment string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := db.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_stat_activity
WHERE datname=current_database() AND pid<>pg_backend_pid()
  AND position($1 in query)>0 AND wait_event_type='Lock')`, fragment).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("query containing %q did not block", fragment)
		case <-ticker.C:
		}
	}
}

func TestRecipientDisableCommitsBeforeForward(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	source := f.create(t, f.alice, f.aliceDevice, "disable-first-source", "forward", messages.Permanent, false)
	disableTx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer disableTx.Rollback(ctx) //nolint:errcheck
	if _, err = disableTx.Exec(ctx, `UPDATE users SET status='DISABLED' WHERE id=$1`, f.bob); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, sendErr := f.service.Forward(ctx, f.alice, f.aliceDevice, messages.ForwardCommand{SourceID: source.ID, RecipientID: f.bob, ExpectedVersion: source.Version, IdempotencyKey: "disable-first-forward"})
		result <- sendErr
	}()
	waitForBlockedQuery(t, f.db, "SELECT status FROM users WHERE id=$1 FOR SHARE")
	if err = disableTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, messages.ErrRecipientUnavailable) {
		t.Fatalf("forward after disable=%v", err)
	}
}

func TestRecipientDeleteCommitsBeforeDirectSend(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deleteTx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTx.Rollback(ctx) //nolint:errcheck
	if _, err = deleteTx.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.bob); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, sendErr := f.service.DirectSend(ctx, f.alice, f.aliceDevice, messages.DirectSendCommand{RecipientID: f.bob, Body: "direct", BodyFormat: messages.Text, IdempotencyKey: "delete-first-direct"})
		result <- sendErr
	}()
	waitForBlockedQuery(t, f.db, "SELECT status FROM users WHERE id=$1 FOR SHARE")
	if err = deleteTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, messages.ErrRecipientUnavailable) {
		t.Fatalf("direct send after delete=%v", err)
	}
}

func TestDirectSendCommitsBeforeConcurrentDisable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const advisoryKey int64 = 734920311
	if _, err := f.db.Exec(ctx, `CREATE FUNCTION p3_block_message_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(734920311);
  RETURN NEW;
END $$;
CREATE TRIGGER p3_block_message_insert BEFORE INSERT ON messages
FOR EACH ROW EXECUTE FUNCTION p3_block_message_insert()`); err != nil {
		t.Fatal(err)
	}
	blocker, err := f.db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blockerTx.Rollback(ctx) //nolint:errcheck
	if _, err = blockerTx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryKey); err != nil {
		t.Fatal(err)
	}
	sendResult := make(chan struct {
		receipt messages.MessageDeliveryReceipt
		err     error
	}, 1)
	go func() {
		receipt, sendErr := f.service.DirectSend(ctx, f.alice, f.aliceDevice, messages.DirectSendCommand{RecipientID: f.bob, Body: "direct wins", BodyFormat: messages.Text, IdempotencyKey: "send-first-direct"})
		sendResult <- struct {
			receipt messages.MessageDeliveryReceipt
			err     error
		}{receipt: receipt, err: sendErr}
	}()
	waitForBlockedQuery(t, f.db, "INSERT INTO messages")
	disableResult := make(chan error, 1)
	go func() {
		_, disableErr := f.db.Exec(ctx, `UPDATE users SET status='DISABLED' WHERE id=$1 /* p3-disable-after-send */`, f.bob)
		disableResult <- disableErr
	}()
	waitForBlockedQuery(t, f.db, "p3-disable-after-send")
	if err = blockerTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	sent := <-sendResult
	if sent.err != nil || sent.receipt.MessageID == uuid.Nil {
		t.Fatalf("send result=%+v err=%v", sent.receipt, sent.err)
	}
	if err = <-disableResult; err != nil {
		t.Fatal(err)
	}
	var owner uuid.UUID
	var status string
	if err = f.db.QueryRow(ctx, `SELECT m.owner_id,u.status FROM messages m JOIN users u ON u.id=m.owner_id WHERE m.id=$1`, sent.receipt.MessageID).Scan(&owner, &status); err != nil {
		t.Fatal(err)
	}
	if owner != f.bob || status != "DISABLED" {
		t.Fatalf("owner=%s status=%s", owner, status)
	}
}
