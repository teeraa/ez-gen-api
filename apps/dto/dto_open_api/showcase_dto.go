package dto_open_api

// ShowcaseItemDTO is served without authentication on the landing page.
type ShowcaseItemDTO struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Slot         string  `json:"slot"`      // reel | editor | character | product
	MediaType    string  `json:"mediaType"` // e.g. video/mp4 or image/png
	Subtitle     *string `json:"subtitle"`
	MediaURL     string  `json:"mediaUrl"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	AspectRatio  string  `json:"aspectRatio"`
	Duration     int     `json:"duration"`
	ExpiresAt    string  `json:"expiresAt"`
}
