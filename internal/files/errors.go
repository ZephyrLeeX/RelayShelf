package files

import "errors"

var (
	ErrAttachmentNotFound = errors.New("attachment not found")
	ErrThumbnailNotFound  = errors.New("thumbnail not found")
	ErrStorageUnavailable = errors.New("storage unavailable")
	ErrStorageIntegrity   = errors.New("storage integrity error")
	ErrRange              = errors.New("invalid range")
)
