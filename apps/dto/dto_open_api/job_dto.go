package dto_open_api

// JobDTO is the API response for a single generation job.
type JobDTO struct {
	JobID       string  `json:"jobId"`
	Status      string  `json:"status"`
	Progress    int     `json:"progress"`
	CurrentStep *string `json:"currentStep"`
	// EstimatedSecondsRemaining is null when the job is terminal or the remaining time is unknown.
	EstimatedSecondsRemaining *int       `json:"estimatedSecondsRemaining"`
	Model                     string     `json:"model"`
	AspectRatio               string     `json:"aspectRatio"`
	Duration                  int        `json:"duration"`
	Quality                   string     `json:"quality"`
	CreditsCharged            *int       `json:"creditsCharged"`
	Result                    *JobResult `json:"result"`
	Error                     *JobError  `json:"error"`
	SceneID                   *string    `json:"sceneId"`
	CharacterProfileID        *string    `json:"characterProfileId"`
	StyleProfileID            *string    `json:"styleProfileId"`
	CompiledPrompt            *string    `json:"compiledPrompt"`
	CreatedAt                 string     `json:"createdAt"`
	UpdatedAt                 string     `json:"updatedAt"`
	CompletedAt               *string    `json:"completedAt"`
}

// JobResult carries output URLs once a job succeeds.
type JobResult struct {
	ThumbnailURL string  `json:"thumbnailUrl"`
	MediaURL     string  `json:"mediaUrl"`
	MediaType    string  `json:"mediaType"`
	ExpiresAt    *string `json:"expiresAt"`
}

// JobError carries human-safe failure information.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CreateJobResponseDTO is returned by POST /open/v1/jobs.
type CreateJobResponseDTO struct {
	JobID            string `json:"jobId"`
	Status           string `json:"status"`
	EstimatedCredits *int   `json:"estimatedCredits"`
	CreatedAt        string `json:"createdAt"`
}

// CancelJobResponseDTO is returned by DELETE /open/v1/jobs/:jobId.
type CancelJobResponseDTO struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// JobListDTO wraps paginated job results.
type JobListDTO struct {
	Items []JobDTO `json:"items"`
	Total int      `json:"total"`
	Page  int      `json:"page"`
	Limit int      `json:"limit"`
}
