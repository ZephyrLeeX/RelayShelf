package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditEvent struct {
	ID                                         uuid.UUID
	ActorUserID, TargetID, DeviceID, SessionID *uuid.UUID
	EventType, TargetType, UserAgent, TraceID  string
	IP                                         netip.Addr
	Metadata                                   map[string]any
	CreatedAt                                  time.Time
}

type Repository interface {
	FindUser(context.Context, string) (User, error)
	FindUserByID(context.Context, uuid.UUID) (User, error)
	UpdatePasswordHash(context.Context, uuid.UUID, string, time.Time) error
	GetOwnedDevice(context.Context, uuid.UUID, uuid.UUID) (Device, error)
	CreateDevice(context.Context, Device) (Device, error)
	CreateSession(context.Context, Session, []byte, netip.Addr) (Session, error)
	FindAuthentication(context.Context, []byte) (Authentication, error)
	Touch(context.Context, Authentication, time.Time, time.Time, netip.Addr) error
	ListSessions(context.Context, uuid.UUID) ([]Session, error)
	RevokeOwnedSession(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error)
	ListDevices(context.Context, uuid.UUID) ([]Device, error)
	RenameDevice(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (Device, error)
	ChangePasswordAndRevokeOthers(context.Context, uuid.UUID, uuid.UUID, string, time.Time, AuditEvent) error
	ResetPasswordAndRevokeAll(context.Context, uuid.UUID, string, time.Time, AuditEvent) error
	Audit(context.Context, AuditEvent) error
}

type PostgreSQLRepository struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

func NewPostgreSQLRepository(pool *pgxpool.Pool) *PostgreSQLRepository {
	return &PostgreSQLRepository{pool: pool, q: generated.New(pool)}
}
func pgu(value uuid.UUID) pgtype.UUID        { return pgtype.UUID{Bytes: value, Valid: true} }
func pgt(value time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: value, Valid: true} }
func ptrUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgu(*value)
}
func textValue(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
func ipPtr(value netip.Addr) *netip.Addr {
	if !value.IsValid() {
		return nil
	}
	return &value
}
func noRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func fromUser(row generated.User) User {
	return User{ID: uuid.UUID(row.ID.Bytes), Username: row.Username, DisplayName: row.DisplayName, PasswordHash: row.PasswordHash, IsAdmin: row.IsAdmin, Status: row.Status}
}
func fromDevice(row generated.Device) Device {
	return Device{ID: uuid.UUID(row.ID.Bytes), UserID: uuid.UUID(row.UserID.Bytes), Name: row.Name, UserAgent: row.UserAgent, FirstSeenAt: row.FirstSeenAt.Time, LastSeenAt: row.LastSeenAt.Time}
}
func fromSession(row generated.Session) Session {
	var revoked *time.Time
	if row.RevokedAt.Valid {
		value := row.RevokedAt.Time
		revoked = &value
	}
	lastIP := ""
	if row.LastIp != nil {
		lastIP = row.LastIp.String()
	}
	return Session{ID: uuid.UUID(row.ID.Bytes), UserID: uuid.UUID(row.UserID.Bytes), DeviceID: uuid.UUID(row.DeviceID.Bytes), CreatedAt: row.CreatedAt.Time, LastSeenAt: row.LastSeenAt.Time, ExpiresAt: row.ExpiresAt.Time, AbsoluteExpiresAt: row.AbsoluteExpiresAt.Time, LastIP: lastIP, RevokedAt: revoked}
}

func (r *PostgreSQLRepository) FindUser(ctx context.Context, name string) (User, error) {
	row, err := r.q.GetUserByUsername(ctx, name)
	return fromUser(row), noRows(err)
}
func (r *PostgreSQLRepository) FindUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	row, err := r.q.GetUserByID(ctx, pgu(id))
	return fromUser(row), noRows(err)
}
func (r *PostgreSQLRepository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string, now time.Time) error {
	n, err := r.q.UpdateUserPasswordHash(ctx, generated.UpdateUserPasswordHashParams{ID: pgu(id), PasswordHash: hash, UpdatedAt: pgt(now)})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
func (r *PostgreSQLRepository) GetOwnedDevice(ctx context.Context, id, userID uuid.UUID) (Device, error) {
	row, err := r.q.GetOwnedDevice(ctx, generated.GetOwnedDeviceParams{ID: pgu(id), UserID: pgu(userID)})
	return fromDevice(row), noRows(err)
}
func (r *PostgreSQLRepository) CreateDevice(ctx context.Context, d Device) (Device, error) {
	row, err := r.q.CreateDevice(ctx, generated.CreateDeviceParams{ID: pgu(d.ID), UserID: pgu(d.UserID), Name: d.Name, UserAgent: d.UserAgent, FirstSeenAt: pgt(d.FirstSeenAt)})
	return fromDevice(row), err
}
func (r *PostgreSQLRepository) CreateSession(ctx context.Context, s Session, hash []byte, ip netip.Addr) (Session, error) {
	row, err := r.q.CreateSession(ctx, generated.CreateSessionParams{ID: pgu(s.ID), UserID: pgu(s.UserID), DeviceID: pgu(s.DeviceID), TokenHash: append([]byte(nil), hash...), ExpiresAt: pgt(s.ExpiresAt), AbsoluteExpiresAt: pgt(s.AbsoluteExpiresAt), LastSeenAt: pgt(s.LastSeenAt), LastIp: ipPtr(ip)})
	return fromSession(row), err
}
func (r *PostgreSQLRepository) FindAuthentication(ctx context.Context, hash []byte) (Authentication, error) {
	row, err := r.q.GetAuthByTokenHash(ctx, hash)
	if err != nil {
		return Authentication{}, noRows(err)
	}
	var revoked *time.Time
	if row.RevokedAt.Valid {
		v := row.RevokedAt.Time
		revoked = &v
	}
	uid, did, sid := uuid.UUID(row.UserID.Bytes), uuid.UUID(row.DeviceID.Bytes), uuid.UUID(row.SessionID.Bytes)
	return Authentication{User: User{ID: uid, Username: row.Username, DisplayName: row.DisplayName, IsAdmin: row.IsAdmin, Status: row.UserStatus}, Device: Device{ID: did, UserID: uid, Name: row.DeviceName, UserAgent: row.UserAgent, FirstSeenAt: row.FirstSeenAt.Time, LastSeenAt: row.DeviceLastSeenAt.Time}, Session: Session{ID: sid, UserID: uid, DeviceID: did, CreatedAt: row.SessionCreatedAt.Time, LastSeenAt: row.SessionLastSeenAt.Time, ExpiresAt: row.ExpiresAt.Time, AbsoluteExpiresAt: row.AbsoluteExpiresAt.Time, LastIP: fmt.Sprint(row.LastIp), RevokedAt: revoked}}, nil
}
func (r *PostgreSQLRepository) Touch(ctx context.Context, a Authentication, seen, expires time.Time, ip netip.Addr) error {
	n, err := r.q.TouchSession(ctx, generated.TouchSessionParams{ID: pgu(a.Session.ID), LastSeenAt: pgt(seen), ExpiresAt: pgt(expires), LastIp: ipPtr(ip)})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAuthenticationRequired
	}
	return r.q.TouchDevice(ctx, generated.TouchDeviceParams{ID: pgu(a.Device.ID), UserID: pgu(a.User.ID), LastSeenAt: pgt(seen)})
}
func (r *PostgreSQLRepository) ListSessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	rows, err := r.q.ListOwnedSessions(ctx, pgu(userID))
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, Session{ID: uuid.UUID(row.ID.Bytes), UserID: userID, DeviceID: uuid.UUID(row.DeviceID.Bytes), CreatedAt: row.CreatedAt.Time, LastSeenAt: row.LastSeenAt.Time, ExpiresAt: row.ExpiresAt.Time, AbsoluteExpiresAt: row.AbsoluteExpiresAt.Time, LastIP: fmt.Sprint(row.LastIp)})
	}
	return out, nil
}
func (r *PostgreSQLRepository) RevokeOwnedSession(ctx context.Context, id, userID uuid.UUID, now time.Time) (bool, error) {
	n, err := r.q.RevokeOwnedSession(ctx, generated.RevokeOwnedSessionParams{ID: pgu(id), UserID: pgu(userID), RevokedAt: pgt(now)})
	return n > 0, err
}
func (r *PostgreSQLRepository) ListDevices(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	rows, err := r.q.ListOwnedDevices(ctx, pgu(userID))
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDevice(row))
	}
	return out, nil
}
func (r *PostgreSQLRepository) RenameDevice(ctx context.Context, id, userID uuid.UUID, name string, now time.Time) (Device, error) {
	row, err := r.q.RenameOwnedDevice(ctx, generated.RenameOwnedDeviceParams{ID: pgu(id), UserID: pgu(userID), Name: name, UpdatedAt: pgt(now)})
	return fromDevice(row), noRows(err)
}

func insertAudit(ctx context.Context, q *generated.Queries, event AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	return q.InsertAuditLog(ctx, generated.InsertAuditLogParams{ID: pgu(event.ID), ActorUserID: ptrUUID(event.ActorUserID), EventType: event.EventType, TargetType: textValue(event.TargetType), TargetID: ptrUUID(event.TargetID), Ip: ipPtr(event.IP), UserAgent: textValue(event.UserAgent), DeviceID: ptrUUID(event.DeviceID), SessionID: ptrUUID(event.SessionID), TraceID: textValue(event.TraceID), Metadata: metadata, CreatedAt: pgt(event.CreatedAt)})
}
func (r *PostgreSQLRepository) Audit(ctx context.Context, event AuditEvent) error {
	return insertAudit(ctx, r.q, event)
}
func (r *PostgreSQLRepository) tx(ctx context.Context, fn func(*generated.Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(r.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *PostgreSQLRepository) ChangePasswordAndRevokeOthers(ctx context.Context, userID, currentSessionID uuid.UUID, hash string, now time.Time, audit AuditEvent) error {
	return r.tx(ctx, func(q *generated.Queries) error {
		if n, err := q.UpdateUserPasswordHash(ctx, generated.UpdateUserPasswordHashParams{ID: pgu(userID), PasswordHash: hash, UpdatedAt: pgt(now)}); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		if err := q.RevokeOtherSessions(ctx, generated.RevokeOtherSessionsParams{UserID: pgu(userID), ID: pgu(currentSessionID), RevokedAt: pgt(now)}); err != nil {
			return err
		}
		return insertAudit(ctx, q, audit)
	})
}
func (r *PostgreSQLRepository) ResetPasswordAndRevokeAll(ctx context.Context, userID uuid.UUID, hash string, now time.Time, audit AuditEvent) error {
	return r.tx(ctx, func(q *generated.Queries) error {
		if n, err := q.UpdateUserPasswordHash(ctx, generated.UpdateUserPasswordHashParams{ID: pgu(userID), PasswordHash: hash, UpdatedAt: pgt(now)}); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		if err := q.RevokeAllUserSessions(ctx, generated.RevokeAllUserSessionsParams{UserID: pgu(userID), RevokedAt: pgt(now)}); err != nil {
			return err
		}
		return insertAudit(ctx, q, audit)
	})
}
