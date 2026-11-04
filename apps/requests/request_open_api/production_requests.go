package request_open_api

import "github.com/ez-gen/ez-gen-api/apps/models"

type CreateProjectRequest struct {
	Title       string `json:"title" validate:"max=200"`
	Brief       string `json:"brief" validate:"required,min=1,max=10000"`
	AspectRatio string `json:"aspectRatio" validate:"omitempty,oneof=16:9 9:16 1:1 4:3 3:4 21:9"`
}
type UpdateProjectRequest struct {
	Revision    int     `json:"revision" validate:"required,min=1"`
	Title       *string `json:"title" validate:"omitempty,min=1,max=200"`
	Brief       *string `json:"brief" validate:"omitempty,min=1,max=10000"`
	AspectRatio *string `json:"aspectRatio" validate:"omitempty,oneof=16:9 9:16 1:1 4:3 3:4 21:9"`
}
type CreateProductionSceneRequest struct {
	Title            string                `json:"title" validate:"max=200"`
	Description      string                `json:"description" validate:"required,min=1,max=2000"`
	ReferenceAssetID *string               `json:"referenceAssetId" validate:"omitempty,uuid4"`
	VideoSettings    *models.VideoSettings `json:"videoSettings"`
}
type SaveEditorRequest struct {
	Revision  int              `json:"revision" validate:"required,min=1"`
	EditDraft models.EditDraft `json:"editDraft"`
}
type ReorderScenesRequest struct {
	SceneIDs []string `json:"sceneIds" validate:"max=100,unique,dive,uuid4"`
}
