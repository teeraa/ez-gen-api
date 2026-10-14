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
	ErrNotShareable  = errors.New("job has no stored output to share")

	// Export errors
	ErrSceneHasNoOutput  = errors.New("one or more scenes have no completed video output")
	ErrExportNotReady    = errors.New("export has no output yet")
	ErrInvalidFileType   = errors.New("invalid file type")
	ErrFileTooLarge      = errors.New("file too large")
)
