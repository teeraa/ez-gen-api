package request_open_api

type MarketplaceListQuery struct {
	Q      string `query:"q"      validate:"max=100"`
	Status string `query:"status" validate:"omitempty,oneof=active draft archived"`
}

// MarketplaceShopRequest is used for both create and full-replace update.
type MarketplaceShopRequest struct {
	Name           string  `json:"name"           validate:"required,max=80"`
	Description    string  `json:"description"    validate:"max=1000"`
	Category       string  `json:"category"       validate:"max=60"`
	LogoUploadID   *string `json:"logoUploadId"   validate:"omitempty,uuid"`
	Phone          string  `json:"phone"          validate:"max=30"`
	Email          string  `json:"email"          validate:"omitempty,email,max=120"`
	LineID         string  `json:"lineId"         validate:"max=60"`
	Website        string  `json:"website"        validate:"max=500"`
	Address        string  `json:"address"        validate:"max=500"`
	BrandTone      string  `json:"brandTone"      validate:"max=300"`
	TargetAudience string  `json:"targetAudience" validate:"max=300"`
	TiktokShopURL  string  `json:"tiktokShopUrl"  validate:"max=500"`
	ShopeeURL      string  `json:"shopeeUrl"      validate:"max=500"`
	LazadaURL      string  `json:"lazadaUrl"      validate:"max=500"`
	Status         string  `json:"status"         validate:"omitempty,oneof=active inactive"`
}

// MarketplaceProductRequest is used for both create and full-replace update.
// ImageUploadIDs is the full ordered image list: ids already on the product are kept, new ones are attached.
type MarketplaceProductRequest struct {
	Name           string   `json:"name"           validate:"required,max=120"`
	Description    string   `json:"description"    validate:"max=2000"`
	SellingPoints  string   `json:"sellingPoints"  validate:"max=1000"`
	Category       string   `json:"category"       validate:"max=60"`
	SKU            string   `json:"sku"            validate:"max=60"`
	Price          float64  `json:"price"          validate:"gte=0,lte=100000000"`
	Stock          int      `json:"stock"          validate:"gte=0,lte=100000000"`
	Status         string   `json:"status"         validate:"omitempty,oneof=active draft archived"`
	ImageUploadIDs []string `json:"imageUploadIds" validate:"max=6,unique,dive,uuid"`
	TiktokShopURL  string   `json:"tiktokShopUrl"  validate:"max=500"`
	ShopeeURL      string   `json:"shopeeUrl"      validate:"max=500"`
	LazadaURL      string   `json:"lazadaUrl"      validate:"max=500"`
}

type MarketplaceContentRequest struct {
	Kind    string  `json:"kind"    validate:"required,oneof=promo_video short_film"`
	Title   string  `json:"title"   validate:"required,max=120"`
	Brief   string  `json:"brief"   validate:"max=4000"`
	RefType string  `json:"refType" validate:"omitempty,oneof=director_series film_series"`
	RefID   *string `json:"refId"   validate:"omitempty,uuid"`
}

type MarketplacePublishRequest struct {
	ContentID *string `json:"contentId" validate:"omitempty,uuid"`
	Platform  string  `json:"platform"  validate:"required,oneof=tiktok_shop shopee lazada"`
	Caption   string  `json:"caption"   validate:"max=2200"`
	PostURL   string  `json:"postUrl"   validate:"max=500"`
	PostedOn  string  `json:"postedOn"  validate:"required,len=10"`
}

type MarketplaceSaleRow struct {
	Platform string  `json:"platform" validate:"required,oneof=tiktok_shop shopee lazada"`
	Date     string  `json:"date"     validate:"required,len=10"`
	Units    int     `json:"units"    validate:"gte=0,lte=10000000"`
	Orders   int     `json:"orders"   validate:"gte=0,lte=10000000"`
	Revenue  float64 `json:"revenue"  validate:"gte=0,lte=100000000000"`
}

// MarketplaceSalesRequest upserts one figure per platform and day; a CSV import sends many rows.
type MarketplaceSalesRequest struct {
	Rows []MarketplaceSaleRow `json:"rows" validate:"required,min=1,max=400,dive"`
}
