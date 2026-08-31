//go:build integration

package users_test

import (
	"context"
	"testing"
	"time"

	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
	"github.com/google/uuid"
)

func TestRecipientDirectoryUsesRecentActivityAndSearchesAllActiveUsers(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	service := users.NewDirectoryService(db)
	base := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	requester := uuid.Must(uuid.NewV7())
	type seed struct {
		id          uuid.UUID
		username    string
		displayName string
		status      string
		lastSeen    time.Time
	}
	rows := []seed{
		{requester, "requester", "Requester", "ACTIVE", base.Add(10 * time.Hour)},
		{uuid.Must(uuid.NewV7()), "alice", "Alice Chen", "ACTIVE", base.Add(2 * time.Hour)},
		{uuid.Must(uuid.NewV7()), "bob", "Bob Stone", "ACTIVE", base.Add(4 * time.Hour)},
		{uuid.Must(uuid.NewV7()), "carol", "Carol", "DISABLED", base.Add(8 * time.Hour)},
		{uuid.Must(uuid.NewV7()), "needle", "Archived Match", "ACTIVE", base.Add(time.Hour)},
	}
	for index, row := range rows {
		if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status,created_at,updated_at)VALUES($1,$2,$3,'hash',$4,$5,$5)`, row.id, row.username, row.displayName, row.status, base.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		deviceID, sessionID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		if _, err := db.Exec(ctx, `INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at)VALUES($1,$2,'test','test',$3,$3)`, deviceID, row.id, row.lastSeen); err != nil {
			t.Fatal(err)
		}
		tokenHash := make([]byte, 32)
		tokenHash[0] = byte(index + 1)
		if _, err := db.Exec(ctx, `INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at,created_at)VALUES($1,$2,$3,$4,$5,$5,$6,$6)`, sessionID, row.id, deviceID, tokenHash, base.Add(24*time.Hour), row.lastSeen); err != nil {
			t.Fatal(err)
		}
	}

	defaultItems, err := service.ListRecipients(ctx, requester, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultItems) != 3 || defaultItems[0].Username != "bob" || defaultItems[1].Username != "alice" || defaultItems[2].Username != "needle" {
		t.Fatalf("default recipients=%+v", defaultItems)
	}
	searched, err := service.ListRecipients(ctx, requester, "archived", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 1 || searched[0].Username != "needle" {
		t.Fatalf("searched recipients=%+v", searched)
	}
}
