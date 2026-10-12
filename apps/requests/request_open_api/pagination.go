package request_open_api

// PaginationQuery is the common page/limit query struct.
type PaginationQuery struct {
	Page  int `query:"page"  validate:"min=1"`
	Limit int `query:"limit" validate:"min=1,max=50"`
}

func (p *PaginationQuery) Defaults() {
	if p.Page == 0 {
		p.Page = 1
	}
	if p.Limit == 0 {
		p.Limit = 20
	}
}
