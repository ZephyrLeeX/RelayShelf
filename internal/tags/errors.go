package tags

import "errors"

var (
	ErrNotFound   = errors.New("tag not found")
	ErrValidation = errors.New("validation error")
	ErrDuplicate  = errors.New("tag already exists")
)
