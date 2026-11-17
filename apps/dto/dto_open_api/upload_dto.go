package dto_open_api

// UploadDTO is the response for POST /api/v1/uploads.
type UploadDTO struct {
	UploadID  string `json:"uploadId"`
	MimeType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
	ExpiresAt string `json:"expiresAt"`
}
