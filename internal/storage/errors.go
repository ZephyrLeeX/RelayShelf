package storage

import "errors"

var (
	ErrNotFound             = errors.New("storage object not found")
	ErrInvalidKey           = errors.New("invalid storage key")
	ErrUnavailable          = errors.New("storage unavailable")
	ErrFull                 = errors.New("storage full")
	ErrIntegrity            = errors.New("storage integrity error")
	ErrDifferentFilesystems = errors.New("commit temp and objects are on different filesystems")
)
