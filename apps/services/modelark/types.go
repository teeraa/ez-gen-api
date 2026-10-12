package modelark

// Application-domain types used by the ModelArk service adapter.
// These types are intentionally decoupled from the SDK's generated structs
// so the rest of the application doesn't import provider-specific packages.

type ContentRole string

const (
	RoleFirstFrame      ContentRole = "first_frame"
	RoleLastFrame       ContentRole = "last_frame"
	RoleReferenceImage  ContentRole = "reference_image"
	RoleReferenceVideo  ContentRole = "reference_video"
)

type ContentItemType string

const (
	ContentTypeText     ContentItemType = "text"
	ContentTypeImageURL ContentItemType = "image_url"
	ContentTypeVideoURL ContentItemType = "video_url"
)

type ContentItem struct {
	Type ContentItemType
	Text string   // set when Type == "text"
	URL  string   // set when Type == "image_url" or "video_url"
	Role ContentRole
}

// CreateTaskRequest is the application-domain representation of a generation request.
// All fields are validated before reaching this point.
type CreateTaskRequest struct {
	Model          string
	Content        []ContentItem
	Ratio          string
	DurationSeconds int64
	Resolution     string
	OptimizePrompt bool
	GenerateAudio  bool
	Watermark      bool
	ReturnLastFrame bool
	ExpiresAfterSeconds int64
}

// TaskResult is the application-domain result of a completed task poll.
type TaskResult struct {
	ProviderTaskID string
	Status         ProviderStatus

	VideoURL     string // ephemeral — must be downloaded immediately
	LastFrameURL string // ephemeral — thumbnail

	RevisedPrompt  string
	ErrorCode      string
	ErrorMessage   string

	DurationSeconds int64
	Resolution      string
}

// ProviderStatus mirrors the SDK's TaskStatus constants
// without exposing the SDK type to the rest of the application.
type ProviderStatus string

const (
	ProviderStatusQueued    ProviderStatus = "queued"
	ProviderStatusRunning   ProviderStatus = "running"
	ProviderStatusSucceeded ProviderStatus = "succeeded"
	ProviderStatusFailed    ProviderStatus = "failed"
	ProviderStatusCancelled ProviderStatus = "cancelled"
)
