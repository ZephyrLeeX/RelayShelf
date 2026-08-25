//go:build integration

package messages_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/tags"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type fixture struct {
	db                                           *pgxpool.Pool
	service                                      *messages.Service
	tags                                         *tags.Service
	clock                                        *fixedClock
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

func TestDirectSendForwardSensitiveAndIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	direct := messages.DirectSendCommand{RecipientID: f.bob, Body: "sent secret", BodyFormat: messages.Markdown, Sensitive: true, IdempotencyKey: "direct"}
	first, err := f.service.DirectSend(ctx, f.alice, f.aliceDevice, direct)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.service.DirectSend(ctx, f.alice, f.aliceDevice, direct)
	if err != nil || again.ID != first.ID {
		t.Fatalf("replay=%v %v", again.ID, err)
	}
	direct.Body = "changed"
	if _, err = f.service.DirectSend(ctx, f.alice, f.aliceDevice, direct); !errors.Is(err, messages.ErrIdempotencyKeyReused) {
		t.Fatalf("reuse=%v", err)
	}
	var receiverOwner, provenance uuid.UUID
	var sourceMessage *uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT owner_id,source_user_id,source_message_id FROM messages WHERE id=$1`, first.ID).Scan(&receiverOwner, &provenance, &sourceMessage); err != nil {
		t.Fatal(err)
	}
	if receiverOwner != f.bob || provenance != f.alice || sourceMessage != nil {
		t.Fatal("direct-send ownership or provenance mismatch")
	}
	var metadata string
	if err = f.db.QueryRow(ctx, `SELECT response_metadata::text FROM idempotency_keys WHERE user_id=$1 AND operation='message.direct-send' AND key='direct'`, f.alice).Scan(&metadata); err != nil || strings.Contains(metadata, "sent secret") {
		t.Fatalf("unsafe idempotency metadata=%q %v", metadata, err)
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
	plain, _, err := f.service.Reveal(ctx, f.bob, destination.ID)
	if err != nil || plain != "forward secret" {
		t.Fatalf("destination reveal=%q %v", plain, err)
	}
	var sourceCipher, destinationCipher []byte
	if err = f.db.QueryRow(ctx, `SELECT body_ciphertext FROM messages WHERE id=$1`, source.ID).Scan(&sourceCipher); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(ctx, `SELECT body_ciphertext FROM messages WHERE id=$1`, destination.ID).Scan(&destinationCipher); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sourceCipher, destinationCipher) || destination.SourceMessageID == nil || *destination.SourceMessageID != source.ID {
		t.Fatal("forward copied ciphertext or lost provenance")
	}
	replayed, err := f.service.Forward(ctx, f.alice, f.aliceDevice, forward)
	if err != nil || replayed.ID != destination.ID {
		t.Fatalf("forward replay=%+v %v", replayed, err)
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
