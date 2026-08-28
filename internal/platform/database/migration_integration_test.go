//go:build integration

package database_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationsAndCompatibility(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewEmptyDatabase(t)
	if err := database.CheckCompatible(ctx, db); err == nil {
		t.Fatal("empty database is compatible")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate must be a no-op: %v", err)
	}
	current, err := database.CurrentVersion(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := database.LatestVersion()
	if err != nil || current != latest {
		t.Fatalf("current=%d latest=%d err=%v", current, latest, err)
	}
	if err := database.CheckCompatible(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", latest); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckCompatible(ctx, db); err == nil {
		t.Fatal("older database is compatible")
	}
	if _, err := db.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES ($1)", latest+1); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckCompatible(ctx, db); err == nil {
		t.Fatal("newer database is compatible")
	}
}

func TestPhase5UploadHandoffIndexMigrations(t *testing.T) {
	ctx := context.Background()

	t.Run("existing clean v2 database", func(t *testing.T) {
		db := postgresutil.NewEmptyDatabase(t)
		applyReleasedV2(t, ctx, db)
		assertVersion(t, ctx, db, 2)
		assertPhase5IndexCount(t, ctx, db, 0)

		applyReleasedMigration(t, ctx, db, 3, "000003_phase5_upload_handoff_indexes.sql")
		assertVersion(t, ctx, db, 3)
		assertPhase5Indexes(t, ctx, db)
	})

	t.Run("fresh database", func(t *testing.T) {
		db := postgresutil.NewEmptyDatabase(t)
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatalf("migrate fresh database: %v", err)
		}
		assertVersion(t, ctx, db, 6)
		assertPhase5Indexes(t, ctx, db)

		var businessTables int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
WHERE table_schema = 'public' AND table_name = ANY($1)`, []string{
			"users", "devices", "sessions", "messages", "tags", "message_tags", "file_objects", "message_attachments", "file_derivatives", "upload_sessions", "upload_parts", "idempotency_keys", "background_jobs", "system_settings", "audit_logs",
		}).Scan(&businessTables); err != nil || businessTables != 15 {
			t.Fatalf("Phase 1 business tables=%d want=15 err=%v", businessTables, err)
		}
	})

	t.Run("drifted v2 compatibility", func(t *testing.T) {
		db := postgresutil.NewEmptyDatabase(t)
		applyReleasedV2(t, ctx, db)
		if _, err := db.Exec(ctx, `
CREATE INDEX upload_sessions_completed_cleanup_idx
    ON upload_sessions (completed_at, id)
    WHERE status = 'COMPLETED';
CREATE INDEX upload_sessions_handoff_file_idx
    ON upload_sessions (file_object_id, completed_at)
    WHERE status = 'COMPLETED' AND consumed_at IS NULL;`); err != nil {
			t.Fatalf("create drifted v2 indexes: %v", err)
		}
		assertVersion(t, ctx, db, 2)
		assertPhase5Indexes(t, ctx, db)

		applyReleasedMigration(t, ctx, db, 3, "000003_phase5_upload_handoff_indexes.sql")
		assertVersion(t, ctx, db, 3)
		assertPhase5Indexes(t, ctx, db)
	})
}

func TestPhase6SearchIndexMigrations(t *testing.T) {
	ctx := context.Background()
	latest, err := database.LatestVersion()
	if err != nil || latest != 6 {
		t.Fatalf("latest migration version=%d want=6 err=%v", latest, err)
	}

	t.Run("existing clean v3 database", func(t *testing.T) {
		db := postgresutil.NewEmptyDatabase(t)
		applyReleasedV2(t, ctx, db)
		applyReleasedMigration(t, ctx, db, 3, "000003_phase5_upload_handoff_indexes.sql")
		assertVersion(t, ctx, db, 3)
		assertPhase6IndexCount(t, ctx, db, 0)
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatalf("migrate v3 to v4: %v", err)
		}
		assertVersion(t, ctx, db, 6)
		assertPhase6Indexes(t, ctx, db)
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatalf("repeat migration: %v", err)
		}
		assertVersion(t, ctx, db, 6)
		assertPhase6Indexes(t, ctx, db)
	})

	t.Run("fresh database", func(t *testing.T) {
		db := postgresutil.NewEmptyDatabase(t)
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatalf("migrate fresh database: %v", err)
		}
		assertVersion(t, ctx, db, 6)
		assertPhase5Indexes(t, ctx, db)
		assertPhase6Indexes(t, ctx, db)
	})
}

func TestReleasedMigrationHistoryIsImmutable(t *testing.T) {
	want := map[string]string{
		"000001_initial_schema.sql":                "3eb8cff7d93ac0798cd8d180b45aad52c414792797e1e3344225d4b22d25b742",
		"000002_pg_trgm.sql":                       "3c041b2c922fb8c60ae41085a7f999c0981fd1c443665d54d0dcc1ba7955a165",
		"000003_phase5_upload_handoff_indexes.sql": "cb6c90012d4b54bd180467a02f13a98e041e8cd9b32924287990d8e847f90c07",
		"000004_phase6_search_indexes.sql":         "4018f66f9680fce5b6f2a59bee5db88f9079007121c4bdddd1a27b1f4f7a05f1",
	}
	for name, expected := range want {
		contents, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		if got := hex.EncodeToString(digest[:]); got != expected {
			t.Fatalf("released migration %s digest=%s want=%s", name, got, expected)
		}
	}
}

func TestPhase7JobMigration(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewEmptyDatabase(t)
	applyReleasedV2(t, ctx, db)
	applyReleasedMigration(t, ctx, db, 3, "000003_phase5_upload_handoff_indexes.sql")
	applyReleasedMigration(t, ctx, db, 4, "000004_phase6_search_indexes.sql")
	assertVersion(t, ctx, db, 4)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate v4 to v5: %v", err)
	}
	assertVersion(t, ctx, db, 6)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("repeat migrate: %v", err)
	}
	var indexCount, constraintCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname='background_jobs_thumbnail_subject_unique_idx'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("thumbnail unique index count=%d err=%v", indexCount, err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conname='background_jobs_thumbnail_subject_check'`).Scan(&constraintCount); err != nil || constraintCount != 1 {
		t.Fatalf("thumbnail check count=%d err=%v", constraintCount, err)
	}
	jobID := "018f0000-0000-7000-8000-000000000001"
	sourceID := "018f0000-0000-7000-8000-000000000002"
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,next_run_at) VALUES($1,'GENERATE_THUMBNAIL','FILE_OBJECT',$2,'PENDING',now())`, jobID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,next_run_at) VALUES('018f0000-0000-7000-8000-000000000003','GENERATE_THUMBNAIL','FILE_OBJECT',$1,'PENDING',now())`, sourceID); err == nil {
		t.Fatal("duplicate thumbnail lifecycle accepted")
	}
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,next_run_at) VALUES('018f0000-0000-7000-8000-000000000004','GENERATE_THUMBNAIL','FILE_OBJECT','PENDING',now())`); err == nil {
		t.Fatal("thumbnail without subject accepted")
	}
}

func applyReleasedV2(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, "CREATE TABLE schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"); err != nil {
		t.Fatalf("create migration metadata: %v", err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer conn.Release()
	for version, name := range []string{"000001_initial_schema.sql", "000002_pg_trgm.sql"} {
		sql, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read released migration %s: %v", name, err)
		}
		migration := database.Migration{Version: int64(version + 1), Name: name, SQL: string(sql)}
		if err := database.ApplyMigration(ctx, conn, migration); err != nil {
			t.Fatalf("apply released migration %s: %v", name, err)
		}
	}
}

func applyReleasedMigration(t *testing.T, ctx context.Context, db *pgxpool.Pool, version int64, name string) {
	t.Helper()
	contents, err := migrations.Files.ReadFile(name)
	if err != nil {
		t.Fatalf("read released migration %s: %v", name, err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer conn.Release()
	if err = database.ApplyMigration(ctx, conn, database.Migration{Version: version, Name: name, SQL: string(contents)}); err != nil {
		t.Fatalf("apply released migration %s: %v", name, err)
	}
}

func assertVersion(t *testing.T, ctx context.Context, db *pgxpool.Pool, want int64) {
	t.Helper()
	got, err := database.CurrentVersion(ctx, db)
	if err != nil || got != want {
		t.Fatalf("schema version=%d want=%d err=%v", got, want, err)
	}
}

func assertPhase5IndexCount(t *testing.T, ctx context.Context, db *pgxpool.Pool, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_indexes
WHERE schemaname = 'public' AND tablename = 'upload_sessions'
AND indexname IN ('upload_sessions_completed_cleanup_idx', 'upload_sessions_handoff_file_idx')`).Scan(&got); err != nil || got != want {
		t.Fatalf("Phase 5 index count=%d want=%d err=%v", got, want, err)
	}
}

func assertPhase5Indexes(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	assertPhase5IndexCount(t, ctx, db, 2)
	assertIndexDefinition(t, ctx, db, "upload_sessions_completed_cleanup_idx", "completed_at,id", "STATUS='COMPLETED'")
	assertIndexDefinition(t, ctx, db, "upload_sessions_handoff_file_idx", "file_object_id,completed_at", "STATUS='COMPLETED'", "CONSUMED_ATISNULL")
}

func assertPhase6IndexCount(t *testing.T, ctx context.Context, db *pgxpool.Pool, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_indexes
WHERE schemaname = 'public'
AND indexname IN ('tags_normalized_name_trgm_idx', 'message_tags_tag_message_idx', 'messages_owner_active_created_idx')`).Scan(&got); err != nil || got != want {
		t.Fatalf("Phase 6 index count=%d want=%d err=%v", got, want, err)
	}
}

func assertPhase6Indexes(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	assertPhase6IndexCount(t, ctx, db, 3)
	assertIndexDefinition(t, ctx, db, "message_tags_tag_message_idx", "tag_id,message_id")
	assertIndexDefinition(t, ctx, db, "messages_owner_active_created_idx", "owner_id,created_at,id", "TRASHED_ATISNULL")
	for _, name := range []string{"message_tags_tag_message_idx", "messages_owner_active_created_idx"} {
		var method string
		if err := db.QueryRow(ctx, `SELECT access_method.amname FROM pg_class index_class JOIN pg_am access_method ON access_method.oid=index_class.relam WHERE index_class.relname=$1`, name).Scan(&method); err != nil || method != "btree" {
			t.Fatalf("index %s method=%s want=btree err=%v", name, method, err)
		}
	}
	var ownerIndexDefinition string
	if err := db.QueryRow(ctx, `SELECT pg_get_indexdef(index_class.oid) FROM pg_class index_class WHERE index_class.relname='messages_owner_active_created_idx'`).Scan(&ownerIndexDefinition); err != nil || !strings.Contains(strings.ToUpper(ownerIndexDefinition), "CREATED_AT DESC") || !strings.Contains(strings.ToUpper(ownerIndexDefinition), "ID DESC") {
		t.Fatalf("owner search index ordering=%q err=%v", ownerIndexDefinition, err)
	}
	var method, opclass string
	if err := db.QueryRow(ctx, `SELECT access_method.amname, operator_class.opcname
FROM pg_class index_class
JOIN pg_index index ON index.indexrelid = index_class.oid
JOIN pg_class table_class ON table_class.oid = index.indrelid
JOIN pg_am access_method ON access_method.oid = index_class.relam
JOIN pg_opclass operator_class ON operator_class.oid = index.indclass[0]
WHERE index_class.relname = 'tags_normalized_name_trgm_idx'`).Scan(&method, &opclass); err != nil {
		t.Fatal(err)
	}
	if method != "gin" || opclass != "gin_trgm_ops" {
		t.Fatalf("tag search index method=%s opclass=%s", method, opclass)
	}
}

func assertIndexDefinition(t *testing.T, ctx context.Context, db *pgxpool.Pool, name, wantColumns string, wantPredicates ...string) {
	t.Helper()
	var columns []string
	var predicate *string
	err := db.QueryRow(ctx, `SELECT array_agg(attribute.attname ORDER BY key.ordinality),
       pg_get_expr(index.indpred, index.indrelid)
FROM pg_class index_class
JOIN pg_index index ON index.indexrelid = index_class.oid
CROSS JOIN LATERAL unnest(index.indkey) WITH ORDINALITY AS key(attnum, ordinality)
JOIN pg_attribute attribute ON attribute.attrelid = index.indrelid AND attribute.attnum = key.attnum
WHERE index_class.relname = $1
GROUP BY index.indpred, index.indrelid`, name).Scan(&columns, &predicate)
	if err != nil {
		t.Fatalf("read index %s definition: %v", name, err)
	}
	if got := strings.Join(columns, ","); got != wantColumns {
		t.Fatalf("index %s columns=%s want=%s", name, got, wantColumns)
	}
	predicateText := ""
	if predicate != nil {
		predicateText = *predicate
	}
	normalized := strings.ToUpper(predicateText)
	normalized = strings.ReplaceAll(normalized, "::TEXT", "")
	normalized = strings.Join(strings.Fields(normalized), "")
	normalized = strings.ReplaceAll(normalized, "(", "")
	normalized = strings.ReplaceAll(normalized, ")", "")
	for _, want := range wantPredicates {
		if !strings.Contains(normalized, want) {
			t.Fatalf("index %s predicate=%q missing %q", name, predicateText, want)
		}
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	migration := database.Migration{Version: 999999, Name: "test_failure", SQL: "CREATE TABLE migration_rollback_probe (id integer); INVALID SQL;"}
	if err := database.ApplyMigration(ctx, conn, migration); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var exists bool
	if err := db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'migration_rollback_probe')").Scan(&exists); err != nil || exists {
		t.Fatalf("migration table exists=%v err=%v", exists, err)
	}
	var recorded bool
	if err := db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", migration.Version).Scan(&recorded); err != nil || recorded {
		t.Fatalf("migration recorded=%v err=%v", recorded, err)
	}
}

func TestAllBusinessTablesExist(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	const expected = 15
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
WHERE table_schema = 'public' AND table_name = ANY($1)`, []string{
		"users", "devices", "sessions", "messages", "tags", "message_tags", "file_objects", "message_attachments", "file_derivatives", "upload_sessions", "upload_parts", "idempotency_keys", "background_jobs", "system_settings", "audit_logs",
	}).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("business tables=%d want %d", count, expected)
	}
}

func TestPhase1SchemaConstraints(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err == nil {
			t.Fatalf("constraint accepted: %s", q)
		}
	}
	const user1 = "00000000-0000-0000-0000-000000000001"
	const user2 = "00000000-0000-0000-0000-000000000002"
	const device = "00000000-0000-0000-0000-000000000010"
	must("INSERT INTO users(id,username,display_name,password_hash,status) VALUES ($1,'one','One','x','ACTIVE'),($2,'two','Two','x','ACTIVE')", user1, user2)
	must("INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at) VALUES ($1,$2,'d','u',now(),now())", device, user1)

	must("INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at) VALUES ('00000000-0000-0000-0000-000000000011',$1,$2,decode(repeat('01',32),'hex'),now(),now(),now())", user1, device)
	reject("INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at) VALUES ('00000000-0000-0000-0000-000000000012',$1,$2,decode(repeat('01',32),'hex'),now(),now(),now())", user1, device)
	reject("INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at) VALUES ('00000000-0000-0000-0000-000000000013',$1,$2,'x',now(),now(),now())", user1, device)

	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle) VALUES ('00000000-0000-0000-0000-000000000020',$1,'plain','TEXT',true,'PERMANENT')", user1)
	reject("INSERT INTO messages(id,owner_id,body_ciphertext,body_nonce,body_encryption_version,body_format,sensitive,lifecycle) VALUES ('00000000-0000-0000-0000-000000000021',$1,'x','short',1,'TEXT',true,'PERMANENT')", user1)
	must("INSERT INTO messages(id,owner_id,body_ciphertext,body_nonce,body_encryption_version,body_format,sensitive,lifecycle) VALUES ('00000000-0000-0000-0000-000000000022',$1,'x',decode(repeat('00',12),'hex'),1,'TEXT',true,'PERMANENT')", user1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,is_favorite,expires_at) VALUES ('00000000-0000-0000-0000-000000000023',$1,'x','TEXT',false,'TEMPORARY',true,now())", user1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at) VALUES ('00000000-0000-0000-0000-000000000024',$1,'x','TEXT',false,'PERMANENT',now())", user1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle) VALUES ('00000000-0000-0000-0000-000000000025',$1,'x','TEXT',false,'TEMPORARY')", user1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,trashed_at) VALUES ('00000000-0000-0000-0000-000000000026',$1,'x','TEXT',false,'PERMANENT',now())", user1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,purge_at) VALUES ('00000000-0000-0000-0000-000000000027',$1,'x','TEXT',false,'PERMANENT',now())", user1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,version) VALUES ('00000000-0000-0000-0000-000000000028',$1,'x','TEXT',false,'PERMANENT',0)", user1)
	must("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle) VALUES ('00000000-0000-0000-0000-000000000029',$1,'x','TEXT',false,'PERMANENT')", user1)

	must("INSERT INTO upload_sessions(id,user_id,original_filename,expected_size,chunk_size,status,expires_at,consumed_at,consumed_message_id) VALUES ('00000000-0000-0000-0000-000000000030',$1,'x',0,1,'COMPLETED',now(),now(),'00000000-0000-0000-0000-000000000029')", user1)
	must("DELETE FROM messages WHERE id = '00000000-0000-0000-0000-000000000029'")
	var consumedAtPresent, consumedMessageAbsent bool
	if err := db.QueryRow(ctx, "SELECT consumed_at IS NOT NULL, consumed_message_id IS NULL FROM upload_sessions WHERE id = '00000000-0000-0000-0000-000000000030'").Scan(&consumedAtPresent, &consumedMessageAbsent); err != nil || !consumedAtPresent || !consumedMessageAbsent {
		t.Fatalf("consumption state after message purge: at=%v message_null=%v err=%v", consumedAtPresent, consumedMessageAbsent, err)
	}
	reject("INSERT INTO upload_sessions(id,user_id,original_filename,expected_size,chunk_size,status,expires_at,consumed_message_id) VALUES ('00000000-0000-0000-0000-000000000031',$1,'x',0,1,'COMPLETED',now(),'00000000-0000-0000-0000-000000000022')", user1)

	must("INSERT INTO tags(id,user_id,name,normalized_name,color) VALUES ('00000000-0000-0000-0000-000000000040',$1,'Tag','tag','#fff')", user1)
	reject("INSERT INTO tags(id,user_id,name,normalized_name,color) VALUES ('00000000-0000-0000-0000-000000000041',$1,'Tag','tag','#fff')", user1)
	must("INSERT INTO tags(id,user_id,name,normalized_name,color) VALUES ('00000000-0000-0000-0000-000000000042',$1,'Tag','tag','#fff')", user2)

	reject("INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status) VALUES ('00000000-0000-0000-0000-000000000050','x',0,'x','filesystem','x','PENDING')")
	must("INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status) VALUES ('00000000-0000-0000-0000-000000000050',decode(repeat('00',32),'hex'),0,'x','filesystem','x','PENDING')")
	reject("INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status) VALUES ('00000000-0000-0000-0000-000000000051',decode(repeat('00',32),'hex'),0,'x','filesystem','y','PENDING')")
	must("INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status) VALUES ('00000000-0000-0000-0000-000000000052','00000000-0000-0000-0000-000000000050','THUMB','x','x',0,'PENDING')")
	reject("INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status) VALUES ('00000000-0000-0000-0000-000000000053','00000000-0000-0000-0000-000000000050','THUMB','y','x',0,'PENDING')")
	must("INSERT INTO upload_sessions(id,user_id,original_filename,expected_size,chunk_size,status,expires_at) VALUES ('00000000-0000-0000-0000-000000000054',$1,'x',0,1,'CREATED',now())", user1)
	reject("INSERT INTO upload_parts(upload_session_id,part_number,size_bytes,completed_at) VALUES ('00000000-0000-0000-0000-000000000054',-1,0,now())")

	must("INSERT INTO idempotency_keys(id,user_id,operation,key,request_hash,expires_at) VALUES ('00000000-0000-0000-0000-000000000060',$1,'create','key',decode(repeat('00',32),'hex'),now())", user1)
	reject("INSERT INTO idempotency_keys(id,user_id,operation,key,request_hash,expires_at) VALUES ('00000000-0000-0000-0000-000000000061',$1,'create','key',decode(repeat('01',32),'hex'),now())", user1)
	reject("INSERT INTO idempotency_keys(id,user_id,operation,key,request_hash,expires_at) VALUES ('00000000-0000-0000-0000-000000000062',$1,'other','other','x',now())", user1)
	must("INSERT INTO audit_logs(id,event_type) VALUES ('00000000-0000-0000-0000-000000000063','TEST')")

	reject("INSERT INTO system_settings(id) VALUES (2)")
	reject("INSERT INTO system_settings(id) VALUES (1)")
	var temporary, trash, maxSize, audit, upload int64
	if err := db.QueryRow(ctx, "SELECT temporary_ttl_hours, trash_ttl_hours, max_file_size_bytes, audit_retention_days, upload_retention_hours FROM system_settings WHERE id = 1").Scan(&temporary, &trash, &maxSize, &audit, &upload); err != nil {
		t.Fatal(err)
	}
	if temporary != 72 || trash != 168 || maxSize != 2147483648 || audit != 90 || upload != 24 {
		t.Fatalf("unexpected system defaults: %d %d %d %d %d", temporary, trash, maxSize, audit, upload)
	}

	var ext string
	var indexCount int
	if err := db.QueryRow(ctx, "SELECT extname FROM pg_extension WHERE extname = 'pg_trgm'").Scan(&ext); err != nil || ext != "pg_trgm" {
		t.Fatalf("pg_trgm missing: %v", err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE indexname IN ('messages_body_plaintext_trgm_idx', 'message_attachments_filename_trgm_idx')").Scan(&indexCount); err != nil || indexCount != 2 {
		t.Fatalf("trigram indexes missing: %v", err)
	}
}
