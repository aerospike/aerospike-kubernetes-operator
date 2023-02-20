package errors

import "errors"

var (
	ErrNotFound       = errors.New("not found")
	ErrInvalidOrEmpty = errors.New("invalid or empty")
)
