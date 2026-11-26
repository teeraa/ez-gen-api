package dto_open_api

type PromptDTO struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Category   string `json:"category"`
	Body       string `json:"body"`
	BuiltIn    bool   `json:"builtIn"`
	Bookmarked bool   `json:"bookmarked"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type PromptListDTO struct {
	Items []PromptDTO `json:"items"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

type SkillDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	Steps        []string `json:"steps"`
	Inputs       []string `json:"inputs"`
	Output       string   `json:"output"`
	Directive    string   `json:"directive"`
	BuiltIn      bool     `json:"builtIn"`
	ForkedFromID *string  `json:"forkedFromId"`
	Version      int      `json:"version"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

type SkillListDTO struct {
	Items []SkillDTO `json:"items"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}
