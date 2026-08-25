//go:build integration

package database_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func temporaryDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	bootstrap, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("relayshelf_it_%d", time.Now().UnixNano())
	if _, err := bootstrap.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	url := os.Getenv("DATABASE_URL")
	slash := strings.LastIndex(url, "/")
	if slash < 0 {
		t.Fatal("DATABASE_URL must include database")
	}
	query := strings.Index(url[slash:], "?")
	if query >= 0 {
		url = url[:slash+1] + name + url[slash+query:]
	} else {
		url = url[:slash+1] + name
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = bootstrap.Exec(context.Background(), "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", name)
		_, _ = bootstrap.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name)
		bootstrap.Close(context.Background())
	})
	return pool
}

func TestMigrationsAndCompatibility(t *testing.T) {
	db := temporaryDatabase(t)
	ctx := context.Background()
	if err := database.CheckCompatible(ctx, db); err == nil {
		t.Fatal("empty database is compatible")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
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
	if _, err := db.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES ($1)", latest+1); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckCompatible(ctx, db); err == nil {
		t.Fatal("newer database is compatible")
	}
}

func TestSchemaConstraints(t *testing.T) {
	db := temporaryDatabase(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err == nil {
			t.Fatalf("constraint accepted %s", q)
		}
	}
	uid1, uid2 := "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"
	must("INSERT INTO users(id,username,display_name,password_hash,status) VALUES ($1,'one','One','x','ACTIVE'),($2,'two','Two','x','ACTIVE')", uid1, uid2)
	reject("INSERT INTO users(id,username,display_name,password_hash,status) VALUES ('00000000-0000-0000-0000-000000000003','one','x','x','ACTIVE')")
	device := "00000000-0000-0000-0000-000000000010"
	must("INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at) VALUES ($1,$2,'d','u',now(),now())", device, uid1)
	reject("INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at) VALUES ('00000000-0000-0000-0000-000000000011',$1,$2,'x',now(),now(),now())", uid1, device)
	msg := "00000000-0000-0000-0000-000000000020"
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle) VALUES ($1,$2,'plain','TEXT',true,'PERMANENT')", msg, uid1)
	reject("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,is_favorite,expires_at) VALUES ($1,$2,'x','TEXT',false,'TEMPORARY',true,now())", msg, uid1)
	must("INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle) VALUES ($1,$2,'x','TEXT',false,'PERMANENT')", msg, uid1)
	must("INSERT INTO tags(id,user_id,name,normalized_name,color) VALUES ('00000000-0000-0000-0000-000000000030',$1,'Tag','tag','#fff')", uid1)
	reject("INSERT INTO tags(id,user_id,name,normalized_name,color) VALUES ('00000000-0000-0000-0000-000000000031',$1,'Tag','tag','#fff')", uid1)
	must("INSERT INTO tags(id,user_id,name,normalized_name,color) VALUES ('00000000-0000-0000-0000-000000000032',$1,'Tag','tag','#fff')", uid2)
	reject("INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status) VALUES ('00000000-0000-0000-0000-000000000040','x',0,'x','filesystem','x','PENDING')")
	must("INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status) VALUES ('00000000-0000-0000-0000-000000000040',decode(repeat('00',32),'hex'),0,'x','filesystem','x','PENDING')")
	reject("INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status) VALUES ('00000000-0000-0000-0000-000000000041',decode(repeat('00',32),'hex'),0,'x','filesystem','x','PENDING')")
	var ext string
	var indexCount int
	if err := db.QueryRow(ctx, "SELECT extname FROM pg_extension WHERE extname='pg_trgm'").Scan(&ext); err != nil || ext != "pg_trgm" {
		t.Fatal("pg_trgm missing")
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE indexname IN ('messages_body_plaintext_trgm_idx','message_attachments_filename_trgm_idx')").Scan(&indexCount); err != nil || indexCount != 2 {
		t.Fatal("trigram indexes missing")
	}
}
