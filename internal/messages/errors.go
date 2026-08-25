package messages

import "errors"

var (
	ErrNotFound                  = errors.New("message not found")
	ErrValidation                = errors.New("validation error")
	ErrVersionConflict           = errors.New("message version conflict")
	ErrFavoriteRequiresPermanent = errors.New("favorite requires permanent message")
	ErrNotSensitive              = errors.New("message is not sensitive")
	ErrTrashed                   = errors.New("message is trashed")
	ErrNotTrashed                = errors.New("message is not trashed")
	ErrIdempotencyKeyReused      = errors.New("idempotency key reused")
	ErrRecipientUnavailable      = errors.New("recipient unavailable")
	ErrCrypto                    = errors.New("sensitive body unavailable")
	ErrContentRequired           = errors.New("message content required")
	ErrUploadAlreadyConsumed     = errors.New("upload already consumed")
)
