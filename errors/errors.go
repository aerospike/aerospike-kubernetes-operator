package errors

import "errors"

var (
	NotFoundError = errors.New("not found")
	InvalidOrEmptyError = errors.New("invalid or empty")
)
