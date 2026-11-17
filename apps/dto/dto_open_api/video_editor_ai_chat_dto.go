package dto_open_api

// VideoEditorAIChatToolDTO is one tool the assistant used while answering, for the UI trail.
type VideoEditorAIChatToolDTO struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	OK    bool   `json:"ok"`
}

// VideoEditorAIChatProposalDTO is a timeline change waiting for the user. Proposal is the
// PUT /projects/:id body at BaseRevision. Status: pending, applied, discarded or stale.
type VideoEditorAIChatProposalDTO struct {
	BaseRevision    int                             `json:"baseRevision"`
	Summary         string                          `json:"summary"`
	Operations      []VideoEditorAIEditOperationDTO `json:"operations"`
	Proposal        VideoEditorAIEditProposalDTO    `json:"proposal"`
	Status          string                          `json:"status"`
	AppliedRevision int                             `json:"appliedRevision,omitempty"`
}

type VideoEditorAIChatMessageDTO struct {
	ID        string                        `json:"id"`
	Role      string                        `json:"role"`
	Content   string                        `json:"content"`
	CreatedAt string                        `json:"createdAt"`
	Tools     []VideoEditorAIChatToolDTO    `json:"tools"`
	Proposal  *VideoEditorAIChatProposalDTO `json:"proposal,omitempty"`
	JobID     string                        `json:"jobId,omitempty"`
	Error     string                        `json:"error,omitempty"`
}

// VideoEditorAIChatModelDTO is one model the user may pick (EDITOR_AI_CHAT_MODELS).
// Prices are list USD per 1M tokens, null when unknown.
type VideoEditorAIChatModelDTO struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Label       string   `json:"label"`
	Available   bool     `json:"available"`
	InputPrice  *float64 `json:"inputPricePerMTok"`
	OutputPrice *float64 `json:"outputPricePerMTok"`
}

type VideoEditorAIChatDTO struct {
	Models            []VideoEditorAIChatModelDTO   `json:"models"`
	DefaultModel      string                        `json:"defaultModel"`
	Available         bool                          `json:"available"`
	UnavailableReason string                        `json:"unavailableReason,omitempty"`
	Provider          string                        `json:"provider"`
	Model             string                        `json:"model"`
	CloudNotice       string                        `json:"cloudNotice"`
	MaxMessageLength  int                           `json:"maxMessageLength"`
	Messages          []VideoEditorAIChatMessageDTO `json:"messages"`
}

type VideoEditorAIChatApplyDTO struct {
	Message VideoEditorAIChatMessageDTO `json:"message"`
	Project VideoEditorProjectDTO       `json:"project"`
}

// VideoEditorAIChatEventDTO is one server-sent event of a chat reply.
// Type: status (a tool started), text (final answer), message (the stored reply), error.
type VideoEditorAIChatEventDTO struct {
	Type    string                       `json:"type"`
	Tool    string                       `json:"tool,omitempty"`
	Label   string                       `json:"label,omitempty"`
	Text    string                       `json:"text,omitempty"`
	Message *VideoEditorAIChatMessageDTO `json:"message,omitempty"`
	Code    string                       `json:"code,omitempty"`
}
