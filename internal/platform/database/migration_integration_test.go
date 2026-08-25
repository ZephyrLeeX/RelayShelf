//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
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
