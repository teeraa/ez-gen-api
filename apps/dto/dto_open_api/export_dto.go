package dto_open_api

// ExportDTO is the response shape for a storyboard export job.
type ExportDTO struct {
	ExportID    string  `json:"exportId"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	CompletedAt *string `json:"completedAt"`
	Error       *string `json:"error"`
}

// ExportListDTO is the paginated list response.
type ExportListDTO struct {
	Items []ExportDTO `json:"items"`
	Total int         `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

// ExportDownloadDTO carries a short-lived signed download URL.
type ExportDownloadDTO struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}
