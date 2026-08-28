package users

import "errors"

var (
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidPassword = errors.New("invalid password")
	ErrNotFound        = errors.New("user not found")
	ErrUsernameTaken   = errors.New("username already exists")
	ErrInvalidList     = errors.New("invalid user list request")
	ErrCursorInvalid   = errors.New("user list cursor is invalid")
)
