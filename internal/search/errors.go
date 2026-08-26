package search

import "errors"

var (
	ErrValidation    = errors.New("search validation error")
	ErrQueryTooShort = errors.New("search query term too short")
	ErrQueryTooLong  = errors.New("search query too long")
	ErrTooManyTokens = errors.New("too many search tokens")
	ErrCursorInvalid = errors.New("search cursor invalid")
)
