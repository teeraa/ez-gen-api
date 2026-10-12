package modelark

import "errors"

var (
	ErrTaskNotFound  = errors.New("modelark: task not found")
	ErrProviderError = errors.New("modelark: provider error")
	ErrTimeout       = errors.New("modelark: request timed out")
)
