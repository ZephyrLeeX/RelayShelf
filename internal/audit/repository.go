package audit

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Clock interface{ Now() time.Time }

type dbtx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Recorder struct {
	ids   id.Generator
	clock Clock
}

func NewRecorder(ids id.Generator, clock Clock) *Recorder { return &Recorder{ids: ids, clock: clock} }

func (r *Recorder) Record(ctx context.Context, tx dbtx, event Event) error {
	eventID, err := r.ids.New()
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(event.metadata)
	if err != nil {
		return err
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = r.clock.Now()
	}
	var actorID, deviceID, sessionID *uuid.UUID
	if event.Actor.UserID != uuid.Nil {
		actorID = &event.Actor.UserID
	}
	if event.Actor.DeviceID != uuid.Nil {
		deviceID = &event.Actor.DeviceID
	}
	if event.Actor.SessionID != uuid.Nil {
		sessionID = &event.Actor.SessionID
	}
	var targetID *uuid.UUID
	if event.TargetID != uuid.Nil {
		targetID = &event.TargetID
	}
	var ip any
	if event.Actor.IP.IsValid() {
		ip = event.Actor.IP.String()
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(id,actor_user_id,event_type,target_type,target_id,ip,user_agent,device_id,session_id,trace_id,metadata,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, eventID, actorID, event.Type, nullable(event.TargetType), targetID, ip, nullable(truncate(event.Actor.UserAgent, 512)), deviceID, sessionID, nullable(truncate(event.Actor.TraceID, 128)), metadata, createdAt)
	return err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncate(value string, max int) string {
	for len(value) > max {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

// Cleanup deletes at most batch expired rows. The scheduler repeats this a
// bounded number of times, keeping every transaction short and idempotent.
func Cleanup(ctx context.Context, tx dbtx, now time.Time, batch int) (int64, error) {
	if batch <= 0 {
		return 0, nil
	}
	tag, err := tx.Exec(ctx, `WITH due AS (
SELECT id FROM audit_logs
WHERE created_at < $1::timestamptz-(SELECT audit_retention_days FROM system_settings WHERE id=1)*interval '1 day'
ORDER BY created_at,id LIMIT $2
) DELETE FROM audit_logs a USING due WHERE a.id=due.id`, now, batch)
	return tag.RowsAffected(), err
}

var _ dbtx = (pgx.Tx)(nil)
