//go:build integration && !race

package search

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	targetMessageCount    = 100_000
	targetAttachmentCount = 25_000
	targetTagCount        = 50_000
	targetRelationCount   = 20_000
)

type targetDataset struct {
	db                     *pgxpool.Pool
	now                    time.Time
	userA, userB           uuid.UUID
	filterTagA, filterTagB uuid.UUID
}

func deterministicID(namespace, number uint64) uuid.UUID {
	var value uuid.UUID
	binary.BigEndian.PutUint64(value[0:8], namespace)
	binary.BigEndian.PutUint64(value[8:16], number)
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	return value
}

func seedTargetDataset(t testing.TB) targetDataset {
	t.Helper()
	ctx := context.Background()
	dataset := targetDataset{
		db:         postgresutil.NewDatabase(t),
		now:        time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC),
		userA:      deterministicID(1, 1),
		userB:      deterministicID(1, 2),
		filterTagA: deterministicID(2, 1),
		filterTagB: deterministicID(2, 2),
	}
	if _, err := dataset.db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES
($1,'bench-a','Bench A','unused','ACTIVE'),($2,'bench-b','Bench B','unused','ACTIVE')`, dataset.userA, dataset.userB); err != nil {
		t.Fatal(err)
	}

	copyRows(t, ctx, dataset.db, pgx.Identifier{"messages"}, []string{
		"id", "owner_id", "body_plaintext", "body_ciphertext", "body_nonce", "body_encryption_version",
		"body_format", "detected_type", "sensitive", "lifecycle", "is_favorite", "expires_at",
		"trashed_at", "purge_at", "created_at", "updated_at",
	}, targetMessageCount, func(index int) []any {
		owner := dataset.userA
		if index%2 == 1 {
			owner = dataset.userB
		}
		createdAt := dataset.now.Add(-time.Duration(index) * time.Second)
		body := any(fmt.Sprintf("ordinary relay postgres message number %06d", index))
		if index%1000 == 0 {
			body = fmt.Sprintf("selective needlebody postgres message %06d", index)
		}
		sensitive := index%211 == 0
		var ciphertext, nonce any
		var encryptionVersion any
		if sensitive {
			body = nil
			ciphertext = []byte("synthetic encrypted payload")
			nonce = make([]byte, 12)
			encryptionVersion = int16(1)
		}
		lifecycle := "PERMANENT"
		var expiresAt any
		if index%7 == 0 {
			lifecycle = "TEMPORARY"
			expiresAt = dataset.now.Add(24 * time.Hour)
			if index%77 == 0 {
				expiresAt = dataset.now.Add(-time.Hour)
			}
		}
		favorite := lifecycle == "PERMANENT" && index%13 == 0
		var trashedAt, purgeAt any
		if index%97 == 0 {
			trashed := dataset.now.Add(-time.Hour)
			trashedAt, purgeAt = trashed, trashed.Add(24*time.Hour)
		}
		detectedType := "text"
		if index%5 == 0 {
			detectedType = "sql"
		}
		return []any{deterministicID(3, uint64(index+1)), owner, body, ciphertext, nonce, encryptionVersion,
			"TEXT", detectedType, sensitive, lifecycle, favorite, expiresAt, trashedAt, purgeAt, createdAt, createdAt}
	})

	copyRows(t, ctx, dataset.db, pgx.Identifier{"file_objects"}, []string{
		"id", "sha256", "size_bytes", "detected_mime", "storage_backend", "storage_key", "status", "created_at", "updated_at", "ready_at",
	}, targetAttachmentCount, func(index int) []any {
		id := deterministicID(4, uint64(index+1))
		digest := sha256.Sum256(id[:])
		return []any{id, digest[:], 1024, "application/octet-stream", "filesystem", "objects/" + id.String(), "READY", dataset.now, dataset.now, dataset.now}
	})
	copyRows(t, ctx, dataset.db, pgx.Identifier{"message_attachments"}, []string{
		"id", "message_id", "file_object_id", "original_filename", "display_order", "created_at",
	}, targetAttachmentCount, func(index int) []any {
		messageIndex := index * 4
		filename := fmt.Sprintf("ordinary-file-%06d.bin", index)
		if messageIndex%1000 == 0 {
			filename = fmt.Sprintf("needlefile-%06d.pdf", index)
		}
		return []any{deterministicID(5, uint64(index+1)), deterministicID(3, uint64(messageIndex+1)), deterministicID(4, uint64(index+1)), filename, 0, dataset.now}
	})

	tagRows := targetTagCount
	copyRows(t, ctx, dataset.db, pgx.Identifier{"tags"}, []string{"id", "user_id", "name", "normalized_name", "color", "created_at", "updated_at"}, tagRows, func(index int) []any {
		owner := dataset.userA
		if index%2 == 1 {
			owner = dataset.userB
		}
		id := deterministicID(2, uint64(index+1))
		name := fmt.Sprintf("ordinary-tag-%03d", index)
		if index < 2 {
			name = "NeedleTag"
		}
		return []any{id, owner, name, name, "#123456", dataset.now, dataset.now}
	})
	copyRows(t, ctx, dataset.db, pgx.Identifier{"message_tags"}, []string{"message_id", "tag_id", "created_at"}, targetRelationCount, func(index int) []any {
		messageIndex := index * 5
		tagNumber := uint64((messageIndex % targetTagCount) + 1)
		if messageIndex%1000 == 0 {
			if messageIndex%2 == 0 {
				tagNumber = 1
			} else {
				tagNumber = 2
			}
		}
		return []any{deterministicID(3, uint64(messageIndex+1)), deterministicID(2, tagNumber), dataset.now}
	})
	if _, err := dataset.db.Exec(ctx, "ANALYZE messages; ANALYZE message_attachments; ANALYZE tags; ANALYZE message_tags;"); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	return dataset
}

func copyRows(t testing.TB, ctx context.Context, db *pgxpool.Pool, table pgx.Identifier, columns []string, count int, row func(int) []any) {
	t.Helper()
	copied, err := db.CopyFrom(ctx, table, columns, pgx.CopyFromSlice(count, func(index int) ([]any, error) { return row(index), nil }))
	if err != nil || copied != int64(count) {
		t.Fatalf("copy %s rows=%d want=%d err=%v", table.Sanitize(), copied, count, err)
	}
}

func explainProductionSearch(t testing.TB, dataset targetDataset, query Query) []any {
	t.Helper()
	querySQL, args := buildQuery(dataset.userA, query, dataset.now)
	var raw []byte
	if err := dataset.db.QueryRow(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+querySQL, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func planJSON(t testing.TB, plan []any) string {
	t.Helper()
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertNoWholeTargetSeqScan(t *testing.T, value any) {
	t.Helper()
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			assertNoWholeTargetSeqScan(t, child)
		}
	case map[string]any:
		if node["Node Type"] == "Seq Scan" {
			relation, _ := node["Relation Name"].(string)
			if relation == "messages" || relation == "message_attachments" || relation == "tags" {
				t.Fatalf("selective search used whole %s sequential scan", relation)
			}
		}
		for _, child := range node {
			assertNoWholeTargetSeqScan(t, child)
		}
	}
}

func TestTargetScaleSearchPlans(t *testing.T) {
	dataset := seedTargetDataset(t)
	lifecycle := "PERMANENT"
	queries := []struct {
		name    string
		query   Query
		indexes []string
	}{
		{"body", Query{Tokens: []string{"needlebody"}, Limit: 30}, []string{"messages_body_plaintext_trgm_idx", "message_attachments_filename_trgm_idx", "tags_normalized_name_trgm_idx"}},
		{"filename", Query{Tokens: []string{"needlefile"}, Limit: 30}, []string{"message_attachments_filename_trgm_idx"}},
		{"tag text", Query{Tokens: []string{"needletag"}, Limit: 30}, []string{"tags_normalized_name_trgm_idx", "message_tags_tag_message_idx"}},
		{"multi token", Query{Tokens: []string{"needlebody", "postgres"}, Limit: 30}, []string{"messages_body_plaintext_trgm_idx"}},
		{"text and tag filter", Query{Tokens: []string{"needlebody"}, TagIDs: []uuid.UUID{dataset.filterTagA}, Limit: 30}, []string{"message_tags_tag_message_idx"}},
		{"owner newest", Query{Limit: 30}, []string{"messages_owner_active_created_idx"}},
		{"common filter", Query{Lifecycle: &lifecycle, Limit: 30}, []string{"messages_owner_active_created_idx"}},
	}
	for _, test := range queries {
		t.Run(test.name, func(t *testing.T) {
			plan := explainProductionSearch(t, dataset, test.query)
			encoded := planJSON(t, plan)
			for _, index := range test.indexes {
				if !contains(encoded, index) {
					t.Fatalf("plan missing index %s", index)
				}
			}
			if len(test.query.Tokens) > 0 {
				assertNoWholeTargetSeqScan(t, plan)
			}
		})
	}
}

func TestTargetScaleCommonPostgresPlan(t *testing.T) {
	dataset := seedTargetDataset(t)
	plan := explainProductionSearch(t, dataset, Query{Tokens: []string{"postgres"}, Limit: 30})
	t.Logf("common postgres EXPLAIN: %s", planJSON(t, plan))
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return substring == ""
}
