package request_open_api

type SendVideoEditorAIChat struct {
	Message string `json:"message"`
	// The page shows the cloud notice; nothing is sent to the provider without consent.
	CloudConsent bool `json:"cloudConsent"`
	// Model is a "provider:model" id from the thread's models list; empty = default.
	Model string `json:"model"`
}
