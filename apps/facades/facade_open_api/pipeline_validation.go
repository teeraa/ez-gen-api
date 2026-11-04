package facade_open_api

// PipelineValidationError identifies an actionable input failure without exposing
// provider diagnostics or the user's submitted content.
type PipelineValidationError struct {
	Field   string
	Reason  string
	Message string
}

func (e *PipelineValidationError) Error() string { return e.Message }
func (e *PipelineValidationError) Unwrap() error { return ErrValidation }
func pipelineValidation(field, reason, message string) error {
	return &PipelineValidationError{Field: field, Reason: reason, Message: message}
}
