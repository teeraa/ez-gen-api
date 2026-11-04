package dto_open_api

type UserDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Plan        string `json:"plan"`
	Role        string `json:"role"`
}
type AuthDTO struct {
	AccessToken string  `json:"accessToken"`
	ExpiresAt   string  `json:"expiresAt"`
	User        UserDTO `json:"user"`
}
type AssetDTO struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	AssetType      string  `json:"assetType"`
	Description    string  `json:"description"`
	Prompt         string  `json:"prompt"`
	Scope          string  `json:"scope"`
	ProjectID      *string `json:"projectId"`
	UploadID       *string `json:"uploadId"`
	JobID          *string `json:"jobId"`
	MediaURL       *string `json:"mediaUrl"`
	MediaExpiresAt *string `json:"mediaExpiresAt"`
	Status         string  `json:"status"`
	Version        int     `json:"version"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}
type AssetListDTO struct {
	Items []AssetDTO `json:"items"`
	Total int        `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}
type ImageCapabilitiesDTO struct {
	SupportsCharacterReferenceImage     bool     `json:"supportsCharacterReferenceImage"`
	CharacterReferenceMode              string   `json:"characterReferenceMode"`
	CharacterReferenceUnavailableReason *string  `json:"characterReferenceUnavailableReason"`
	Categories                          []string `json:"categories"`
	Enabled                             bool     `json:"enabled"`
	Model                               string   `json:"model"`
	AspectRatios                        []string `json:"aspectRatios"`
	SupportsReferenceImage              bool     `json:"supportsReferenceImage"`
	UnavailableReason                   *string  `json:"unavailableReason"`
}
