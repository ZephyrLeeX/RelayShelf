package messages

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Clock interface{ Now() time.Time }

type Service struct {
	repo   *PostgreSQLRepository
	ids    id.Generator
	clock  Clock
	cipher BodyCipher
}

func NewService(repo *PostgreSQLRepository, ids id.Generator, clock Clock, cipher BodyCipher) *Service {
	return &Service{repo: repo, ids: ids, clock: clock, cipher: cipher}
}

func validBody(body string) bool {
	return len(body) > 0 && len([]byte(body)) <= MaxBodyBytes && utf8.ValidString(body)
}
func validFormat(value string) bool    { return value == Text || value == Markdown }
func validLifecycle(value string) bool { return value == Temporary || value == Permanent }

func (s *Service) newMessage(ownerID, deviceID uuid.UUID, body, format, lifecycle string, sensitive bool, sourceUser, sourceMessage *uuid.UUID, now time.Time, ttl time.Duration) (Message, error) {
	idValue, err := s.ids.New()
	if err != nil {
		return Message{}, err
	}
	m := Message{ID: idValue, OwnerID: ownerID, BodyFormat: format, Lifecycle: lifecycle, Sensitive: sensitive, SourceUserID: sourceUser, SourceMessageID: sourceMessage, CreatedDeviceID: &deviceID, Version: 1, CreatedAt: now, UpdatedAt: now, Tags: []Tag{}}
	if lifecycle == Temporary {
		value := now.Add(ttl)
		m.ExpiresAt = &value
	}
	if sensitive {
		ciphertext, nonce, version, encryptErr := s.cipher.Encrypt(m.ID, m.OwnerID, []byte(body))
		if encryptErr != nil {
			return Message{}, encryptErr
		}
		m.BodyCiphertext = ciphertext
		m.BodyNonce = nonce
		m.BodyEncryptionVersion = &version
	} else {
		m.BodyPlaintext = &body
	}
	return m, nil
}

func (s *Service) Create(ctx context.Context, ownerID, deviceID uuid.UUID, c CreateCommand) (Message, error) {
	if !validBody(c.Body) || !validFormat(c.BodyFormat) || !validLifecycle(c.Lifecycle) || !validIdempotencyKey(c.IdempotencyKey) {
		return Message{}, ErrValidation
	}
	hash := hashCreate(c)
	now := s.clock.Now()
	var result Message
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		idem, err := claimIdempotency(ctx, tx, ownerID, OperationCreate, c.IdempotencyKey, hash, now)
		if err != nil {
			return err
		}
		if idem.Found {
			result, err = loadOwned(ctx, tx, ownerID, idem.ResourceID, false)
			return err
		}
		if err = validateTags(ctx, tx, ownerID, c.TagIDs); err != nil {
			return err
		}
		temporaryTTL, _, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		result, err = s.newMessage(ownerID, deviceID, c.Body, c.BodyFormat, c.Lifecycle, c.Sensitive, nil, nil, now, temporaryTTL)
		if err != nil {
			return err
		}
		if err = insertMessage(ctx, tx, result); err != nil {
			return err
		}
		if err = replaceTags(ctx, tx, result.ID, c.TagIDs); err != nil {
			return err
		}
		idemID, err := s.ids.New()
		if err != nil {
			return err
		}
		return completeIdempotency(ctx, tx, idemID, ownerID, OperationCreate, c.IdempotencyKey, hash, result.ID, resourceMetadata(result.ID, now), now)
	})
	if err != nil {
		return Message{}, err
	}
	result.Tags, err = loadTags(ctx, s.repo.queries, result.ID)
	return result, err
}

func (s *Service) Detail(ctx context.Context, ownerID, messageID uuid.UUID) (Message, error) {
	m, err := s.repo.Get(ctx, ownerID, messageID)
	if err != nil {
		return Message{}, err
	}
	if m.Sensitive {
		m.BodyPlaintext = nil
	}
	return m, nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID, filter ListFilter, trash bool) (Page, error) {
	if filter.Limit == 0 {
		filter.Limit = DefaultLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxLimit {
		return Page{}, ErrValidation
	}
	if filter.Lifecycle != nil && !validLifecycle(*filter.Lifecycle) {
		return Page{}, ErrValidation
	}
	rows, err := s.repo.List(ctx, ownerID, filter, trash, s.clock.Now())
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: make([]Summary, 0, min(len(rows), filter.Limit))}
	if len(rows) > filter.Limit {
		marker := rows[filter.Limit-1]
		at := marker.CreatedAt
		if trash && marker.TrashedAt != nil {
			at = *marker.TrashedAt
		}
		encoded := EncodeCursor(Cursor{At: at, ID: marker.ID})
		page.NextCursor = &encoded
		rows = rows[:filter.Limit]
	}
	for _, m := range rows {
		summary := Summary{Message: m}
		summary.BodyPlaintext = nil
		if !m.Sensitive && m.BodyPlaintext != nil {
			summary.BodyPreview, summary.BodyTruncated = preview(*m.BodyPlaintext)
		}
		page.Items = append(page.Items, summary)
	}
	return page, nil
}

func (s *Service) mutate(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, fn func(*Message) (bool, error)) (Message, error) {
	if expected < 1 {
		return Message{}, ErrValidation
	}
	now := s.clock.Now()
	var result Message
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		m, err := loadOwned(ctx, tx, ownerID, messageID, true)
		if err != nil {
			return err
		}
		if m.Version != expected {
			return ErrVersionConflict
		}
		changed, err := fn(&m)
		if err != nil {
			return err
		}
		if changed {
			m.Version++
			m.UpdatedAt = now
			if err = saveMessage(ctx, tx, m); err != nil {
				return err
			}
		}
		result = m
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	result.Tags, err = loadTags(ctx, s.repo.queries, result.ID)
	if result.Sensitive {
		result.BodyPlaintext = nil
	}
	return result, err
}

func (s *Service) Edit(ctx context.Context, ownerID, messageID uuid.UUID, c EditCommand) (Message, error) {
	if c.Body == nil && c.BodyFormat == nil && !c.DetectedType.Set && !c.DetectedLanguage.Set {
		return Message{}, ErrValidation
	}
	if c.Body != nil && !validBody(*c.Body) {
		return Message{}, ErrValidation
	}
	if c.BodyFormat != nil && !validFormat(*c.BodyFormat) {
		return Message{}, ErrValidation
	}
	for _, optional := range []OptionalString{c.DetectedType, c.DetectedLanguage} {
		if optional.Set && optional.Value != nil && utf8.RuneCountInString(*optional.Value) > 64 {
			return Message{}, ErrValidation
		}
	}
	return s.mutate(ctx, ownerID, messageID, c.ExpectedVersion, func(m *Message) (bool, error) {
		if m.TrashedAt != nil {
			return false, ErrTrashed
		}
		if m.Sensitive && c.Body != nil {
			return false, ErrValidation
		}
		changed := false
		if c.Body != nil && m.BodyPlaintext != nil && *c.Body != *m.BodyPlaintext {
			m.BodyPlaintext = c.Body
			changed = true
		}
		if c.BodyFormat != nil && *c.BodyFormat != m.BodyFormat {
			m.BodyFormat = *c.BodyFormat
			changed = true
		}
		if c.DetectedType.Set && !optionalEqual(m.DetectedType, c.DetectedType.Value) {
			m.DetectedType = c.DetectedType.Value
			changed = true
		}
		if c.DetectedLanguage.Set && !optionalEqual(m.DetectedLanguage, c.DetectedLanguage.Value) {
			m.DetectedLanguage = c.DetectedLanguage.Value
			changed = true
		}
		return changed, nil
	})
}

func optionalEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *Service) ReplaceTags(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, ids []uuid.UUID) (Message, error) {
	if expected < 1 {
		return Message{}, ErrValidation
	}
	now := s.clock.Now()
	var result Message
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		m, err := loadOwned(ctx, tx, ownerID, messageID, true)
		if err != nil {
			return err
		}
		if m.Version != expected {
			return ErrVersionConflict
		}
		if m.TrashedAt != nil {
			return ErrTrashed
		}
		if err = validateTags(ctx, tx, ownerID, ids); err != nil {
			return err
		}
		current, err := currentTagIDs(ctx, tx, messageID)
		if err != nil {
			return err
		}
		if !sameUUIDSet(current, ids) {
			if err = replaceTags(ctx, tx, messageID, ids); err != nil {
				return err
			}
			m.Version++
			m.UpdatedAt = now
			if err = saveMessage(ctx, tx, m); err != nil {
				return err
			}
		}
		result = m
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	result.Tags, err = loadTags(ctx, s.repo.queries, messageID)
	if result.Sensitive {
		result.BodyPlaintext = nil
	}
	return result, err
}

func (s *Service) MakePermanent(ctx context.Context, ownerID, messageID uuid.UUID, expected int64) (Message, error) {
	return s.mutate(ctx, ownerID, messageID, expected, func(m *Message) (bool, error) {
		if m.TrashedAt != nil {
			return false, ErrTrashed
		}
		if m.Lifecycle == Permanent {
			return false, nil
		}
		m.Lifecycle = Permanent
		m.ExpiresAt = nil
		return true, nil
	})
}

func (s *Service) SetFavorite(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, favorite bool) (Message, error) {
	return s.mutate(ctx, ownerID, messageID, expected, func(m *Message) (bool, error) {
		if m.TrashedAt != nil {
			return false, ErrTrashed
		}
		if favorite && m.Lifecycle != Permanent {
			return false, ErrFavoriteRequiresPermanent
		}
		if m.Favorite == favorite {
			return false, nil
		}
		m.Favorite = favorite
		return true, nil
	})
}

func (s *Service) Trash(ctx context.Context, ownerID, messageID uuid.UUID, expected int64) (Message, error) {
	var ttl time.Duration
	return s.mutateWithSettings(ctx, ownerID, messageID, expected, func(m *Message, temp, trash time.Duration, now time.Time) (bool, error) {
		ttl = trash
		_ = ttl
		if m.TrashedAt != nil {
			return false, nil
		}
		m.TrashedAt = &now
		purge := now.Add(trash)
		m.PurgeAt = &purge
		return true, nil
	})
}

func (s *Service) Restore(ctx context.Context, ownerID, messageID uuid.UUID, expected int64) (Message, error) {
	return s.mutateWithSettings(ctx, ownerID, messageID, expected, func(m *Message, temp, _ time.Duration, now time.Time) (bool, error) {
		if m.TrashedAt == nil {
			return false, ErrNotTrashed
		}
		m.TrashedAt = nil
		m.PurgeAt = nil
		if m.Lifecycle == Permanent {
			m.ExpiresAt = nil
		} else if m.ExpiresAt == nil || !m.ExpiresAt.After(now) {
			expiry := now.Add(temp)
			m.ExpiresAt = &expiry
		}
		return true, nil
	})
}

func (s *Service) mutateWithSettings(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, fn func(*Message, time.Duration, time.Duration, time.Time) (bool, error)) (Message, error) {
	if expected < 1 {
		return Message{}, ErrValidation
	}
	now := s.clock.Now()
	var result Message
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		m, err := loadOwned(ctx, tx, ownerID, messageID, true)
		if err != nil {
			return err
		}
		if m.Version != expected {
			return ErrVersionConflict
		}
		temp, trash, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		changed, err := fn(&m, temp, trash, now)
		if err != nil {
			return err
		}
		if changed {
			m.Version++
			m.UpdatedAt = now
			if err = saveMessage(ctx, tx, m); err != nil {
				return err
			}
		}
		result = m
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	result.Tags, err = loadTags(ctx, s.repo.queries, messageID)
	if result.Sensitive {
		result.BodyPlaintext = nil
	}
	return result, err
}

func (s *Service) SetSensitive(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, sensitive bool) (Message, error) {
	return s.mutate(ctx, ownerID, messageID, expected, func(m *Message) (bool, error) {
		if m.TrashedAt != nil {
			return false, ErrTrashed
		}
		if m.Sensitive == sensitive {
			return false, nil
		}
		if sensitive {
			if m.BodyPlaintext == nil || !validBody(*m.BodyPlaintext) {
				return false, ErrValidation
			}
			ciphertext, nonce, version, err := s.cipher.Encrypt(m.ID, m.OwnerID, []byte(*m.BodyPlaintext))
			if err != nil {
				return false, err
			}
			m.BodyPlaintext = nil
			m.BodyCiphertext = ciphertext
			m.BodyNonce = nonce
			m.BodyEncryptionVersion = &version
			m.Sensitive = true
		} else {
			plain, err := s.cipher.Decrypt(*m)
			if err != nil {
				return false, err
			}
			body := string(plain)
			m.BodyPlaintext = &body
			m.BodyCiphertext = nil
			m.BodyNonce = nil
			m.BodyEncryptionVersion = nil
			m.Sensitive = false
		}
		return true, nil
	})
}

func (s *Service) Reveal(ctx context.Context, ownerID, messageID uuid.UUID) (string, int64, error) {
	m, err := s.repo.Get(ctx, ownerID, messageID)
	if err != nil {
		return "", 0, err
	}
	if !m.Sensitive {
		return "", 0, ErrNotSensitive
	}
	plain, err := s.cipher.Decrypt(m)
	if err != nil {
		return "", 0, err
	}
	return string(plain), m.Version, nil
}

func (s *Service) EditSensitive(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, body string) (Message, error) {
	if !validBody(body) {
		return Message{}, ErrValidation
	}
	return s.mutate(ctx, ownerID, messageID, expected, func(m *Message) (bool, error) {
		if m.TrashedAt != nil {
			return false, ErrTrashed
		}
		if !m.Sensitive {
			return false, ErrNotSensitive
		}
		ciphertext, nonce, version, err := s.cipher.Encrypt(m.ID, m.OwnerID, []byte(body))
		if err != nil {
			return false, err
		}
		m.BodyCiphertext = ciphertext
		m.BodyNonce = nonce
		m.BodyEncryptionVersion = &version
		return true, nil
	})
}

func (s *Service) DirectSend(ctx context.Context, senderID, deviceID uuid.UUID, c DirectSendCommand) (MessageDeliveryReceipt, error) {
	if !validBody(c.Body) || !validFormat(c.BodyFormat) || !validIdempotencyKey(c.IdempotencyKey) {
		return MessageDeliveryReceipt{}, ErrValidation
	}
	hash := hashDirect(c)
	now := s.clock.Now()
	var receipt MessageDeliveryReceipt
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		idem, err := claimIdempotency(ctx, tx, senderID, OperationDirectSend, c.IdempotencyKey, hash, now)
		if err != nil {
			return err
		}
		if idem.Found {
			receipt, err = deliveryReceipt(idem)
			return err
		}
		status, statusErr := generated.New(tx).GetRecipientStatus(ctx, pgu(c.RecipientID))
		if errors.Is(statusErr, pgx.ErrNoRows) || status != "ACTIVE" {
			return ErrRecipientUnavailable
		} else if statusErr != nil {
			return statusErr
		}
		temp, _, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		source := senderID
		result, err := s.newMessage(c.RecipientID, deviceID, c.Body, c.BodyFormat, Temporary, c.Sensitive, &source, nil, now, temp)
		if err != nil {
			return err
		}
		if err = insertMessage(ctx, tx, result); err != nil {
			return err
		}
		idemID, err := s.ids.New()
		if err != nil {
			return err
		}
		if result.ExpiresAt == nil {
			return errors.New("delivery message missing expiry")
		}
		receipt = MessageDeliveryReceipt{MessageID: result.ID, CreatedAt: result.CreatedAt, ExpiresAt: *result.ExpiresAt}
		metadata, err := deliveryMetadata(receipt)
		if err != nil {
			return err
		}
		return completeIdempotency(ctx, tx, idemID, senderID, OperationDirectSend, c.IdempotencyKey, hash, result.ID, metadata, now)
	})
	if err != nil {
		return MessageDeliveryReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) Forward(ctx context.Context, senderID, deviceID uuid.UUID, c ForwardCommand) (MessageDeliveryReceipt, error) {
	if c.ExpectedVersion < 1 || !validIdempotencyKey(c.IdempotencyKey) {
		return MessageDeliveryReceipt{}, ErrValidation
	}
	hash := hashForward(c)
	now := s.clock.Now()
	var receipt MessageDeliveryReceipt
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		idem, err := claimIdempotency(ctx, tx, senderID, OperationForward, c.IdempotencyKey, hash, now)
		if err != nil {
			return err
		}
		if idem.Found {
			receipt, err = deliveryReceipt(idem)
			return err
		}
		source, err := loadOwned(ctx, tx, senderID, c.SourceID, true)
		if err != nil {
			return err
		}
		if source.Version != c.ExpectedVersion {
			return ErrVersionConflict
		}
		if source.TrashedAt != nil {
			return ErrTrashed
		}
		status, statusErr := generated.New(tx).GetRecipientStatus(ctx, pgu(c.RecipientID))
		if errors.Is(statusErr, pgx.ErrNoRows) || status != "ACTIVE" {
			return ErrRecipientUnavailable
		} else if statusErr != nil {
			return statusErr
		}
		var body string
		if source.Sensitive {
			plain, decryptErr := s.cipher.Decrypt(source)
			if decryptErr != nil {
				return decryptErr
			}
			body = string(plain)
		} else if source.BodyPlaintext != nil {
			body = *source.BodyPlaintext
		} else {
			return ErrValidation
		}
		temp, _, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		sourceUser := senderID
		sourceID := source.ID
		result, err := s.newMessage(c.RecipientID, deviceID, body, source.BodyFormat, Temporary, source.Sensitive, &sourceUser, &sourceID, now, temp)
		if err != nil {
			return err
		}
		if err = insertMessage(ctx, tx, result); err != nil {
			return err
		}
		idemID, err := s.ids.New()
		if err != nil {
			return err
		}
		if result.ExpiresAt == nil {
			return errors.New("delivery message missing expiry")
		}
		receipt = MessageDeliveryReceipt{MessageID: result.ID, CreatedAt: result.CreatedAt, ExpiresAt: *result.ExpiresAt}
		metadata, err := deliveryMetadata(receipt)
		if err != nil {
			return err
		}
		return completeIdempotency(ctx, tx, idemID, senderID, OperationForward, c.IdempotencyKey, hash, result.ID, metadata, now)
	})
	if err != nil {
		return MessageDeliveryReceipt{}, err
	}
	return receipt, nil
}

type AuditContext struct {
	DeviceID, SessionID    uuid.UUID
	TraceID, IP, UserAgent string
}

func (s *Service) PermanentlyDelete(ctx context.Context, ownerID, messageID uuid.UUID, a AuditContext) error {
	return s.repo.withTx(ctx, func(tx pgx.Tx) error {
		m, err := loadOwned(ctx, tx, ownerID, messageID, true)
		if err != nil {
			return err
		}
		if m.TrashedAt == nil {
			return ErrNotTrashed
		}
		auditID, err := s.ids.New()
		if err != nil {
			return err
		}
		if err = generated.New(tx).InsertPermanentDeleteAudit(ctx, generated.InsertPermanentDeleteAuditParams{ID: pgu(auditID), ActorUserID: pgu(ownerID), TargetID: pgu(messageID), Column4: a.IP, UserAgent: pgs(&a.UserAgent), Column6: a.DeviceID.String(), Column7: a.SessionID.String(), TraceID: pgs(&a.TraceID), Column9: m.Lifecycle, CreatedAt: pgt(s.clock.Now())}); err != nil {
			return err
		}
		rows, err := generated.New(tx).DeleteTrashedOwnedMessage(ctx, generated.DeleteTrashedOwnedMessageParams{ID: pgu(messageID), OwnerID: pgu(ownerID)})
		if err == nil && rows != 1 {
			return ErrNotFound
		}
		return err
	})
}

func (s *Service) ExpireDueTemporary(ctx context.Context, batch int) (int64, error) {
	if batch < 1 || batch > 500 {
		return 0, ErrValidation
	}
	now := s.clock.Now()
	var affected int64
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		_, trash, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		affected, err = generated.New(tx).ExpireDueTemporary(ctx, generated.ExpireDueTemporaryParams{TrashedAt: pgt(now), Limit: int32(batch), PurgeAt: pgt(now.Add(trash))})
		return err
	})
	return affected, err
}
