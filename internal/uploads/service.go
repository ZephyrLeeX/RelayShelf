package uploads

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/google/uuid"
)

type Service struct {
	repo           Repository
	staging        staging.Provider
	space          staging.SpaceProbe
	ids            id.Generator
	clock          clock.Clock
	locks          *LockRegistry
	writes         *WriteSemaphore
	maxStaging     int64
	minFreeBytes   int64
	minFreePercent int
	stagingLife    sync.RWMutex
}

func NewService(repo Repository, provider staging.Provider, space staging.SpaceProbe, ids id.Generator, now clock.Clock, locks *LockRegistry, maxWrites int, maxStaging, minFreeBytes int64, minFreePercent int) *Service {
	if locks == nil {
		locks = NewLockRegistry()
	}
	return &Service{repo: repo, staging: provider, space: space, ids: ids, clock: now, locks: locks, writes: NewWriteSemaphore(maxWrites), maxStaging: maxStaging, minFreeBytes: minFreeBytes, minFreePercent: minFreePercent}
}

func validFilename(value string) bool {
	return utf8.ValidString(value) && len(value) > 0 && len(value) <= 255 && !strings.ContainsAny(value, "\x00/\\")
}
func validMime(value *string) bool {
	return value == nil || (utf8.ValidString(*value) && len(*value) <= 255 && !strings.ContainsRune(*value, '\x00'))
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, command CreateCommand) (Session, error) {
	if ownerID == uuid.Nil || !validFilename(command.OriginalFilename) || !validMime(command.ClientMime) || command.ExpectedSize < 0 {
		return Session{}, ErrValidation
	}
	_ = s.ExpireDueUploads(ctx, 8)
	uploadID, err := s.ids.New()
	if err != nil {
		return Session{}, err
	}
	created := false
	var result Session
	s.stagingLife.RLock()
	defer s.stagingLife.RUnlock()
	err = s.repo.WithCreateReservation(ctx, func(txCtx context.Context, reservation Reservation, inserter CreateInserter) error {
		if command.ExpectedSize > reservation.Settings.MaxFileSizeBytes {
			return ErrFileTooLarge
		}
		if command.ExpectedSize > s.maxStaging || reservation.ActiveBytes > s.maxStaging-command.ExpectedSize {
			return ErrStagingFull
		}
		space, probeErr := s.space.Probe()
		if probeErr != nil || space.AvailableBytes < 0 || space.TotalBytes <= 0 {
			return ErrStagingUnavailable
		}
		pressure := reservation.ActiveRemaining
		if pressure > math.MaxInt64-command.ExpectedSize {
			return ErrStagingFull
		}
		projected := space.AvailableBytes - pressure - command.ExpectedSize
		minimumPercentBytes := space.TotalBytes/100*int64(s.minFreePercent) + ((space.TotalBytes%100)*int64(s.minFreePercent)+99)/100
		if projected < s.minFreeBytes || projected < 0 || projected < minimumPercentBytes {
			return ErrStagingFull
		}
		now := s.clock.Now()
		result = Session{ID: uploadID, UserID: ownerID, OriginalFilename: command.OriginalFilename, ExpectedSize: command.ExpectedSize, ClientMime: command.ClientMime, ChunkSize: ChunkSize, Status: Created, ExpiresAt: now.Add(time.Duration(reservation.Settings.UploadRetentionHours) * time.Hour), CreatedAt: now, UpdatedAt: now, CompletedParts: []int{}}
		if err := s.staging.Create(uploadID, command.ExpectedSize); err != nil {
			return ErrStagingUnavailable
		}
		created = true
		return inserter.Insert(txCtx, result)
	})
	if err != nil {
		if created {
			_ = s.staging.Delete(uploadID)
		}
		return Session{}, err
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, ownerID, uploadID uuid.UUID) (Session, error) {
	row, err := s.repo.Get(ctx, ownerID, uploadID)
	if err != nil {
		return Session{}, err
	}
	parts, err := s.repo.ListParts(ctx, uploadID)
	if err != nil {
		return Session{}, err
	}
	row.CompletedParts = make([]int, 0, len(parts))
	for _, part := range parts {
		row.CompletedParts = append(row.CompletedParts, part.Number)
	}
	return row, nil
}

func (s *Service) PutPart(ctx context.Context, ownerID, uploadID uuid.UUID, partNumber int, contentLength int64, body io.Reader) error {
	unlock := s.locks.Chunk(uploadID, partNumber)
	defer unlock()
	row, err := s.repo.Get(ctx, ownerID, uploadID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	if !row.ExpiresAt.After(now) || row.Status == Expired {
		return ErrExpired
	}
	if row.Status != Created && row.Status != Uploading {
		return ErrInvalidState
	}
	expected := row.PartSize(partNumber)
	if expected < 0 {
		return ErrPartOutOfRange
	}
	if contentLength >= 0 && contentLength != expected {
		return ErrPartSizeMismatch
	}
	if err = s.repo.InvalidatePart(ctx, uploadID, partNumber); err != nil {
		return err
	}
	if err = s.writes.Acquire(ctx.Done()); err != nil {
		return err
	}
	defer s.writes.Release()
	f, err := s.staging.Open(uploadID)
	if err != nil {
		return ErrStagingUnavailable
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	offset := int64(partNumber) * row.ChunkSize
	writer := &offsetWriter{file: f, offset: offset}
	limited := io.LimitReader(body, expected)
	written, copyErr := io.CopyBuffer(writer, limited, make([]byte, 32<<10))
	if copyErr != nil {
		return copyErr
	}
	if written != expected {
		return ErrPartSizeMismatch
	}
	var extra [1]byte
	n, readErr := io.ReadFull(body, extra[:])
	if n != 0 {
		return ErrPartSizeMismatch
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if err = f.Sync(); err != nil {
		return ErrStagingUnavailable
	}
	err = f.Close()
	closed = true
	if err != nil {
		return ErrStagingUnavailable
	}
	return s.repo.CommitPart(ctx, ownerID, uploadID, partNumber, written, s.clock.Now())
}

type offsetWriter struct {
	file   staging.File
	offset int64
}

func (w *offsetWriter) Write(data []byte) (int, error) {
	n, err := w.file.WriteAt(data, w.offset)
	w.offset += int64(n)
	return n, err
}

func (s *Service) Complete(ctx context.Context, ownerID, uploadID uuid.UUID) (Session, error) {
	unlock := s.locks.Exclusive(uploadID)
	defer unlock()
	result, err := s.repo.Complete(ctx, ownerID, uploadID, s.clock.Now(), func(row Session, parts []Part) error {
		info, statErr := s.staging.Stat(uploadID)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != row.ExpectedSize {
			return ErrStagingCorrupt
		}
		if syncErr := s.staging.Sync(uploadID); syncErr != nil {
			return ErrStagingCorrupt
		}
		if len(parts) != row.PartCount() {
			return ErrIncomplete
		}
		var total int64
		for expectedNumber, part := range parts {
			if part.Number != expectedNumber || part.SizeBytes != row.PartSize(expectedNumber) {
				return ErrIncomplete
			}
			if total > math.MaxInt64-part.SizeBytes {
				return ErrIncomplete
			}
			total += part.SizeBytes
		}
		if total != row.ExpectedSize {
			return ErrIncomplete
		}
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return s.Get(ctx, ownerID, result.ID)
}

func (s *Service) ExpireDueUploads(ctx context.Context, batch int32) error {
	if batch <= 0 {
		return nil
	}
	if err := s.ReconcileStaging(ctx, int(batch)); err != nil {
		return err
	}
	ids, err := s.repo.FindExpired(ctx, s.clock.Now(), batch)
	if err != nil {
		return err
	}
	for _, uploadID := range ids {
		unlock := s.locks.Exclusive(uploadID)
		_, clean, markErr := s.repo.MarkExpired(ctx, uploadID, s.clock.Now())
		if markErr == nil && clean {
			markErr = s.staging.Delete(uploadID)
			if markErr == nil {
				markErr = s.repo.DeleteParts(ctx, uploadID)
			}
		}
		unlock()
		if markErr != nil {
			return markErr
		}
	}
	return nil
}

func (s *Service) ReconcileStaging(ctx context.Context, limit int) error {
	s.stagingLife.Lock()
	defer s.stagingLife.Unlock()
	ids, err := s.staging.OwnedFiles()
	if err != nil {
		return err
	}
	for index, uploadID := range ids {
		if limit > 0 && index >= limit {
			break
		}
		active, activeErr := s.repo.ActiveExists(ctx, uploadID)
		if activeErr != nil {
			return activeErr
		}
		if !active {
			unlock := s.locks.Exclusive(uploadID)
			err = s.staging.Delete(uploadID)
			unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
