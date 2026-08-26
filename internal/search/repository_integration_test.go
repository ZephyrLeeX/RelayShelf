//go:build integration

package search

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationFixture struct {
	db         *pgxpool.Pool
	service    *Service
	now        time.Time
	alice, bob uuid.UUID
}

func newIntegrationFixture(t testing.TB) *integrationFixture {
	t.Helper()
	db := postgresutil.NewDatabase(t)
	fixture := &integrationFixture{db: db, now: time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC), alice: uuid.Must(uuid.NewV7()), bob: uuid.Must(uuid.NewV7())}
	for _, user := range []struct {
		id      uuid.UUID
		name    string
		isAdmin bool
	}{{fixture.alice, "alice", false}, {fixture.bob, "bob", true}} {
		if _, err := db.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,'unused',$3,'ACTIVE')`, user.id, user.name, user.isAdmin); err != nil {
			t.Fatal(err)
		}
	}
	fixture.service = NewService(NewPostgreSQLRepository(db), fixedClock{fixture.now})
	return fixture
}

type messageOptions struct {
	body         *string
	sensitive    bool
	lifecycle    string
	favorite     bool
	detectedType *string
	expiresAt    *time.Time
	trashedAt    *time.Time
	createdAt    time.Time
}

func textPointer(value string) *string { return &value }

func (fixture *integrationFixture) insertMessage(t testing.TB, owner uuid.UUID, options messageOptions) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if options.lifecycle == "" {
		options.lifecycle = messages.Permanent
	}
	if options.createdAt.IsZero() {
		options.createdAt = fixture.now
	}
	var ciphertext, nonce []byte
	var encryptionVersion *int16
	if options.sensitive {
		options.body = nil
		ciphertext = []byte("encrypted-secretword-payload")
		nonce = make([]byte, 12)
		version := int16(1)
		encryptionVersion = &version
	}
	if options.lifecycle == messages.Temporary && options.expiresAt == nil {
		expires := fixture.now.Add(time.Hour)
		options.expiresAt = &expires
	}
	var purgeAt *time.Time
	if options.trashedAt != nil {
		purge := options.trashedAt.Add(24 * time.Hour)
		purgeAt = &purge
	}
	_, err := fixture.db.Exec(context.Background(), `INSERT INTO messages(
id,owner_id,body_plaintext,body_ciphertext,body_nonce,body_encryption_version,body_format,
detected_type,sensitive,lifecycle,is_favorite,expires_at,trashed_at,purge_at,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,'TEXT',$7,$8,$9,$10,$11,$12,$13,$14,$14)`, id, owner, options.body,
		ciphertext, nonce, encryptionVersion, options.detectedType, options.sensitive, options.lifecycle,
		options.favorite, options.expiresAt, options.trashedAt, purgeAt, options.createdAt)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture *integrationFixture) addTag(t testing.TB, owner, messageID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := fixture.db.Exec(context.Background(), `INSERT INTO tags(id,user_id,name,normalized_name,color,created_at,updated_at) VALUES($1,$2,$3,lower($3),'#123456',$4,$4)`, id, owner, name, fixture.now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(context.Background(), `INSERT INTO message_tags(message_id,tag_id) VALUES($1,$2)`, messageID, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture *integrationFixture) addAttachment(t testing.TB, messageID uuid.UUID, name string, order int) uuid.UUID {
	t.Helper()
	fileID, attachmentID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	digest := sha256.Sum256(fileID[:])
	if _, err := fixture.db.Exec(context.Background(), `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,42,'application/octet-stream','filesystem',$3,'READY',$4,$4,$4)`, fileID, digest[:], "objects/"+fileID.String(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(context.Background(), `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order,created_at) VALUES($1,$2,$3,$4,$5,$6)`, attachmentID, messageID, fileID, name, order, fixture.now); err != nil {
		t.Fatal(err)
	}
	return attachmentID
}

func (fixture *integrationFixture) search(t testing.TB, owner uuid.UUID, text string, mutate func(*Query)) Page {
	t.Helper()
	tokens, err := tokenize(text)
	if err != nil {
		t.Fatalf("tokenize %q: %v", text, err)
	}
	query := Query{Tokens: tokens, Limit: 100}
	if mutate != nil {
		mutate(&query)
	}
	page, err := fixture.service.Search(context.Background(), owner, query)
	if err != nil {
		t.Fatalf("search %q: %v", text, err)
	}
	return page
}

func containsMessage(page Page, id uuid.UUID) bool {
	for _, item := range page.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestSearchSourcesANDPrivacyActiveAndSensitiveBoundaries(t *testing.T) {
	fixture := newIntegrationFixture(t)
	aliceBody := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer("PostgreSQL deployment 这是中文测试内容 https://example.com/api/v1/foo SELECT * FROM users /var/lib/postgresql")})
	fixture.insertMessage(t, fixture.bob, messageOptions{body: textPointer("PostgreSQL deployment alpha")})
	cross := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer("postgres")})
	fixture.addAttachment(t, cross, "backup.sql", 0)
	fixture.addAttachment(t, cross, "restore-guide.txt", 1)
	fixture.addTag(t, fixture.alice, cross, "Production")
	sensitiveHidden := fixture.insertMessage(t, fixture.alice, messageOptions{sensitive: true})
	sensitiveMetadata := fixture.insertMessage(t, fixture.alice, messageOptions{sensitive: true})
	fixture.addAttachment(t, sensitiveMetadata, "secretword.pdf", 0)
	trashedAt := fixture.now.Add(-time.Minute)
	trashed := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer("alpha trashed"), trashedAt: &trashedAt})
	expiredAt := fixture.now.Add(-time.Minute)
	expired := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer("alpha expired"), lifecycle: messages.Temporary, expiresAt: &expiredAt})

	for _, query := range []string{"postgres", "中文", "example api foo", "select users", "var postgresql"} {
		page := fixture.search(t, fixture.alice, query, nil)
		if !containsMessage(page, aliceBody) {
			t.Fatalf("query %q did not match body", query)
		}
	}
	if page := fixture.search(t, fixture.alice, "postgres backup production", nil); !containsMessage(page, cross) {
		t.Fatal("cross-field AND did not match")
	}
	if page := fixture.search(t, fixture.alice, "backup restore", nil); !containsMessage(page, cross) {
		t.Fatal("cross-attachment AND did not match")
	}
	if page := fixture.search(t, fixture.alice, "postgres missing", nil); containsMessage(page, cross) {
		t.Fatal("missing AND term matched")
	}
	page := fixture.search(t, fixture.alice, "secretword", nil)
	if containsMessage(page, sensitiveHidden) || !containsMessage(page, sensitiveMetadata) {
		t.Fatalf("sensitive boundaries hidden=%v metadata=%v", containsMessage(page, sensitiveHidden), containsMessage(page, sensitiveMetadata))
	}
	for _, item := range page.Items {
		if item.ID == sensitiveMetadata && (item.BodyPreview != nil || item.BodyTruncated || item.BodyPlaintext != nil) {
			t.Fatalf("sensitive result exposed body: %+v", item)
		}
	}
	page = fixture.search(t, fixture.alice, "alpha", nil)
	if containsMessage(page, trashed) || containsMessage(page, expired) || len(page.Items) != 0 {
		t.Fatalf("inactive/cross-owner content returned: %+v", page.Items)
	}
	if page = fixture.search(t, fixture.bob, "postgres", nil); len(page.Items) != 1 {
		t.Fatalf("admin owner scope count=%d", len(page.Items))
	}
}

func TestSearchLiteralFiltersHydrationAndNoMutation(t *testing.T) {
	fixture := newIntegrationFixture(t)
	typeSQL := "sql"
	target := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer(`100% foo_bar C:\data docker`), favorite: true, detectedType: &typeSQL, createdAt: fixture.now.Add(-time.Hour)})
	temporaryType := "text"
	temporary := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer("docker temporary"), lifecycle: messages.Temporary, detectedType: &temporaryType, createdAt: fixture.now.Add(-30 * time.Minute)})
	tagA := fixture.addTag(t, fixture.alice, target, "Docker")
	tagB := fixture.addTag(t, fixture.alice, target, "生产环境")
	for index := 0; index < 4; index++ {
		fixture.addAttachment(t, target, fmt.Sprintf("file-%d.bin", index), index)
	}
	foreignMessage := fixture.insertMessage(t, fixture.bob, messageOptions{body: textPointer("docker")})
	foreignTag := fixture.addTag(t, fixture.bob, foreignMessage, "Foreign")

	for _, literal := range []string{"100%", "foo_bar", `C:\data`, "docker docker", "生产"} {
		if page := fixture.search(t, fixture.alice, literal, nil); !containsMessage(page, target) {
			t.Fatalf("literal query %q did not match", literal)
		}
	}
	if page := fixture.search(t, fixture.alice, "%%", nil); len(page.Items) != 0 {
		t.Fatal("percent wildcard became match-all")
	}
	if page := fixture.search(t, fixture.alice, "__", nil); len(page.Items) != 0 {
		t.Fatal("underscore wildcard became match-all")
	}
	after, before := fixture.now.Add(-time.Hour), fixture.now
	lifecycle := messages.Permanent
	favorite := true
	detectedType := "SQL"
	page := fixture.search(t, fixture.alice, "docker", func(query *Query) {
		query.Lifecycle = &lifecycle
		query.Favorite = &favorite
		query.TagIDs = []uuid.UUID{tagA, tagB, tagA}
		query.DetectedType = &detectedType
		query.CreatedAfter = &after
		query.CreatedBefore = &before
	})
	if len(page.Items) != 1 || page.Items[0].ID != target || page.Items[0].AttachmentCount != 4 || len(page.Items[0].Attachments) != 3 || len(page.Items[0].Tags) != 2 {
		t.Fatalf("filtered hydrated result=%+v", page.Items)
	}
	if page = fixture.search(t, fixture.alice, "docker", func(query *Query) { query.TagIDs = []uuid.UUID{foreignTag} }); len(page.Items) != 0 {
		t.Fatal("foreign tag filter leaked existence")
	}
	temporaryLifecycle := messages.Temporary
	falseValue := false
	if page = fixture.search(t, fixture.alice, "docker", func(query *Query) { query.Lifecycle = &temporaryLifecycle; query.Favorite = &falseValue }); len(page.Items) != 1 || page.Items[0].ID != temporary {
		t.Fatalf("temporary/favorite=false filter=%+v", page.Items)
	}
	wrongType := "json"
	if page = fixture.search(t, fixture.alice, "docker", func(query *Query) { query.DetectedType = &wrongType }); len(page.Items) != 0 {
		t.Fatal("exact type filter matched wrong type")
	}
	exclusive := fixture.now.Add(-time.Hour)
	if page = fixture.search(t, fixture.alice, "100%", func(query *Query) { query.CreatedBefore = &exclusive }); len(page.Items) != 0 {
		t.Fatal("createdBefore was not exclusive")
	}
	var beforeVersion, afterVersion int64
	if err := fixture.db.QueryRow(context.Background(), `SELECT version FROM messages WHERE id=$1`, target).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	fixture.search(t, fixture.alice, "docker", nil)
	if err := fixture.db.QueryRow(context.Background(), `SELECT version FROM messages WHERE id=$1`, target).Scan(&afterVersion); err != nil || beforeVersion != afterVersion {
		t.Fatalf("search mutated version before=%d after=%d err=%v", beforeVersion, afterVersion, err)
	}
}

func TestSearchCursorStableWithSameTimestampAndCancellation(t *testing.T) {
	fixture := newIntegrationFixture(t)
	for index := 1; index <= 7; index++ {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", index))
		if _, err := fixture.db.Exec(context.Background(), `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,created_at,updated_at) VALUES($1,$2,'cursor body','TEXT',false,'PERMANENT',$3,$3)`, id, fixture.alice, fixture.now); err != nil {
			t.Fatal(err)
		}
	}
	query := Query{Tokens: []string{"cursor"}, Limit: 3}
	first, err := fixture.service.Search(context.Background(), fixture.alice, query)
	if err != nil || len(first.Items) != 3 || first.NextCursor == nil {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	cursor, err := DecodeCursor(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	inserted := fixture.insertMessage(t, fixture.alice, messageOptions{body: textPointer("cursor body"), createdAt: fixture.now.Add(time.Second)})
	query.Cursor = &cursor
	second, err := fixture.service.Search(context.Background(), fixture.alice, query)
	if err != nil || len(second.Items) != 3 {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	seen := map[uuid.UUID]bool{inserted: true}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.ID] {
			t.Fatalf("duplicate or post-page insert returned: %s", item.ID)
		}
		seen[item.ID] = true
	}
	if second.NextCursor == nil {
		t.Fatal("second page missing cursor")
	}
	thirdCursor, err := DecodeCursor(*second.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	query.Cursor = &thirdCursor
	third, err := fixture.service.Search(context.Background(), fixture.alice, query)
	if err != nil || len(third.Items) != 1 || third.NextCursor != nil || seen[third.Items[0].ID] {
		t.Fatalf("third page=%+v err=%v", third, err)
	}
	seen[third.Items[0].ID] = true
	if len(seen) != 8 { // seven original rows plus the intentionally excluded new insert marker.
		t.Fatalf("stable pagination unique IDs=%d want=8", len(seen))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = fixture.service.Search(ctx, fixture.alice, Query{Tokens: []string{"cursor"}}); err == nil {
		t.Fatal("cancelled search succeeded")
	}
}
