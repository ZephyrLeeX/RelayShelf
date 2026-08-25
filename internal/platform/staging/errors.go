package staging

import "errors"

var (
	ErrInvalidUploadID = errors.New("invalid upload id")
	ErrUnavailable     = errors.New("staging unavailable")
)
