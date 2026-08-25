package auth

import "errors"

var (
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrSessionExpired         = errors.New("session expired")
	ErrRateLimited            = errors.New("rate limited")
	ErrForbidden              = errors.New("forbidden")
	ErrNotFound               = errors.New("not found")
	ErrValidation             = errors.New("validation error")
	ErrCSRFInvalid            = errors.New("csrf invalid")
	ErrOriginInvalid          = errors.New("origin invalid")
)
