package uploads

import "errors"

var (
	ErrNotFound           = errors.New("upload not found")
	ErrFileTooLarge       = errors.New("upload file too large")
	ErrStagingFull        = errors.New("upload staging full")
	ErrStagingUnavailable = errors.New("upload staging unavailable")
	ErrExpired            = errors.New("upload expired")
	ErrInvalidState       = errors.New("upload invalid state")
	ErrPartOutOfRange     = errors.New("upload part out of range")
	ErrPartSizeMismatch   = errors.New("upload part size mismatch")
	ErrIncomplete         = errors.New("upload incomplete")
	ErrStagingCorrupt     = errors.New("upload staging corrupt")
	ErrValidation         = errors.New("validation error")
)
