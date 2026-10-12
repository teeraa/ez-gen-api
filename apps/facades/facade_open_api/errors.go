package facade_open_api

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrConflict      = errors.New("conflict")
	ErrRateLimit     = errors.New("rate limit exceeded")
	ErrValidation    = errors.New("validation error")
	ErrProviderError = errors.New("provider error")
	ErrImageRequired = errors.New("reference image required")
	ErrImageExpired  = errors.New("reference image expired")
	ErrModelInvalid  = errors.New("model not supported for this mode")
)
