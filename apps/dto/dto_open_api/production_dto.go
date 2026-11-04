package dto_open_api

import "github.com/ez-gen/ez-gen-api/apps/models"

type ProjectDTO struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Brief       string           `json:"brief"`
	AspectRatio string           `json:"aspectRatio"`
	Revision    int              `json:"revision"`
	EditDraft   models.EditDraft `json:"editDraft"`
	CreatedAt   string           `json:"createdAt"`
	UpdatedAt   string           `json:"updatedAt"`
}
type ProjectDetailDTO struct {
	ProjectDTO
	Scenes  []SceneDTO  `json:"scenes"`
	Exports []ExportDTO `json:"exports"`
}
type ProjectListDTO struct {
	Items []ProjectDTO `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
}
type ProductionSceneListDTO struct {
	Items []SceneDTO `json:"items"`
	Total int        `json:"total"`
}
type VideoModelDTO struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	ProviderModel          string   `json:"providerModel"`
	AspectRatios           []string `json:"aspectRatios"`
	MinDuration            int      `json:"minDuration"`
	MaxDuration            int      `json:"maxDuration"`
	Qualities              []string `json:"qualities"`
	SupportsImageToVideo   bool     `json:"supportsImageToVideo"`
	SupportsReferenceImage bool     `json:"supportsReferenceImage"`
	SupportsAudio          bool     `json:"supportsAudio"`
}
type ExportCapabilitiesDTO struct {
	SupportsTextOverlays bool     `json:"supportsTextOverlays"`
	Available            bool     `json:"available"`
	Formats              []string `json:"formats"`
	Resolutions          []string `json:"resolutions"`
	AspectRatios         []string `json:"aspectRatios"`
}
type VideoCapabilitiesDTO struct {
	Available bool                  `json:"available"`
	Models    []VideoModelDTO       `json:"models"`
	Export    ExportCapabilitiesDTO `json:"export"`
}
