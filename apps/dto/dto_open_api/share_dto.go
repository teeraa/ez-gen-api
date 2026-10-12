package dto_open_api

// ShareDTO is returned to the owner of a share link.
type ShareDTO struct {
	ShareID   string `json:"shareId"`
	JobID     string `json:"jobId"`
	URL       string `json:"url"`
	ViewCount int64  `json:"viewCount"`
	CreatedAt string `json:"createdAt"`
}

// PublicShareDTO is served without authentication, so it carries only what the share
// page needs to play the video: no prompt, user, job or provider identifiers.
type PublicShareDTO struct {
	MediaURL     string  `json:"mediaUrl"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	DownloadURL  string  `json:"downloadUrl"`
	MediaType    string  `json:"mediaType"`
	AspectRatio  string  `json:"aspectRatio"`
	Duration     int     `json:"duration"`
	ExpiresAt    string  `json:"expiresAt"`
	CreatedAt    string  `json:"createdAt"`
}

// DownloadDTO is a short-lived URL that makes the browser save the video.
type DownloadDTO struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	ExpiresAt string `json:"expiresAt"`
}
