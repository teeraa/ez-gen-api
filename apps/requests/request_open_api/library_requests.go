package request_open_api

type ListLibraryQuery struct {
	Scope    string `query:"scope"    validate:"omitempty,oneof=all mine builtin saved"`
	Category string `query:"category" validate:"omitempty,oneof=video image product character camera"`
	Q        string `query:"q"        validate:"max=100"`
}

// PromptRequest is used for both create and full-replace update.
type PromptRequest struct {
	Title    string `json:"title"    validate:"required,max=60"`
	Category string `json:"category" validate:"required,oneof=video image product character camera"`
	Body     string `json:"body"     validate:"required,max=2000"`
}

// SkillRequest is used for both create and full-replace update; ForkedFromID is honoured on create only.
type SkillRequest struct {
	Name         string   `json:"name"         validate:"required,max=60"`
	Description  string   `json:"description"  validate:"max=300"`
	Steps        []string `json:"steps"        validate:"required,min=1,max=8,dive,max=200"`
	Inputs       []string `json:"inputs"       validate:"max=6,dive,oneof=character product style voice music script"`
	Output       string   `json:"output"       validate:"required,oneof=video_9_16 video_16_9 storyboard image_set"`
	ForkedFromID *string  `json:"forkedFromId" validate:"omitempty,uuid"`
}
