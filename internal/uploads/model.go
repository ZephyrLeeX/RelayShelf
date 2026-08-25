package uploads

import (
	"time"

	"github.com/google/uuid"
)

const (
	ChunkSize  int64 = 8 << 20
	Created          = "CREATED"
	Uploading        = "UPLOADING"
	Completing       = "COMPLETING"
	Completed        = "COMPLETED"
	Failed           = "FAILED"
	Expired          = "EXPIRED"
)

type Session struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	OriginalFilename string
	ExpectedSize     int64
	ClientMime       *string
	ChunkSize        int64
	Status           string
	ExpiresAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedParts   []int
	FileObjectID     *uuid.UUID
	CompletedAt      *time.Time
}

func (s Session) PartCount() int {
	if s.ExpectedSize == 0 {
		return 0
	}
	return int((s.ExpectedSize + s.ChunkSize - 1) / s.ChunkSize)
}

func (s Session) PartSize(part int) int64 {
	if part < 0 || part >= s.PartCount() {
		return -1
	}
	if part == s.PartCount()-1 {
		return s.ExpectedSize - int64(part)*s.ChunkSize
	}
	return s.ChunkSize
}

type Part struct {
	Number      int
	SizeBytes   int64
	CompletedAt time.Time
}

type CreateCommand struct {
	OriginalFilename string
	ExpectedSize     int64
	ClientMime       *string
}

type Settings struct {
	MaxFileSizeBytes     int64
	UploadRetentionHours int32
}

type Reservation struct {
	Settings        Settings
	ActiveBytes     int64
	ActiveRemaining int64
}
