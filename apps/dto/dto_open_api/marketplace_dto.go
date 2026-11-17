package dto_open_api

type MarketplaceShopDTO struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Category       string  `json:"category"`
	LogoUploadID   *string `json:"logoUploadId"`
	LogoURL        *string `json:"logoUrl"`
	Phone          string  `json:"phone"`
	Email          string  `json:"email"`
	LineID         string  `json:"lineId"`
	Website        string  `json:"website"`
	Address        string  `json:"address"`
	BrandTone      string  `json:"brandTone"`
	TargetAudience string  `json:"targetAudience"`
	TiktokShopURL  string  `json:"tiktokShopUrl"`
	ShopeeURL      string  `json:"shopeeUrl"`
	LazadaURL      string  `json:"lazadaUrl"`
	Status         string  `json:"status"`
	ProductCount   int64   `json:"productCount"`
	// Trends is filled on the single-shop read only.
	Trends    *MarketplaceTrendsDTO `json:"trends"`
	CreatedAt string                `json:"createdAt"`
	UpdatedAt string                `json:"updatedAt"`
}

type MarketplaceShopListDTO struct {
	Items []MarketplaceShopDTO `json:"items"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Limit int                  `json:"limit"`
}

type MarketplaceImageDTO struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type MarketplaceProductDTO struct {
	ID            string                `json:"id"`
	ShopID        string                `json:"shopId"`
	ShopName      string                `json:"shopName,omitempty"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	SellingPoints string                `json:"sellingPoints"`
	Category      string                `json:"category"`
	SKU           string                `json:"sku"`
	Price         float64               `json:"price"`
	Stock         int                   `json:"stock"`
	Status        string                `json:"status"`
	Images        []MarketplaceImageDTO `json:"images"`
	TiktokShopURL string                `json:"tiktokShopUrl"`
	ShopeeURL     string                `json:"shopeeUrl"`
	LazadaURL     string                `json:"lazadaUrl"`
	// Sales over the last 30 days and the number of linked contents; list reads only.
	RecentUnits   int64   `json:"recentUnits"`
	RecentRevenue float64 `json:"recentRevenue"`
	ContentCount  int64   `json:"contentCount"`
	// Suggestion is filled on the single-product read only.
	Suggestion *MarketplaceSuggestionDTO `json:"suggestion"`
	CreatedAt  string                    `json:"createdAt"`
	UpdatedAt  string                    `json:"updatedAt"`
}

type MarketplaceProductListDTO struct {
	Items []MarketplaceProductDTO `json:"items"`
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
	Limit int                     `json:"limit"`
}

type MarketplaceContentDTO struct {
	ID        string  `json:"id"`
	ProductID string  `json:"productId"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Brief     string  `json:"brief"`
	RefType   string  `json:"refType"`
	RefID     *string `json:"refId"`
	CreatedAt string  `json:"createdAt"`
}

type MarketplaceContentListDTO struct {
	Items []MarketplaceContentDTO `json:"items"`
}

type MarketplacePublishListDTO struct {
	Items []MarketplacePublishDTO `json:"items"`
}

type MarketplacePublishDTO struct {
	ID        string  `json:"id"`
	ProductID string  `json:"productId"`
	ContentID *string `json:"contentId"`
	Platform  string  `json:"platform"`
	Caption   string  `json:"caption"`
	PostURL   string  `json:"postUrl"`
	PostedOn  string  `json:"postedOn"`
	CreatedAt string  `json:"createdAt"`
}

type MarketplaceSaleDTO struct {
	ID       string  `json:"id"`
	Platform string  `json:"platform"`
	Date     string  `json:"date"`
	Units    int     `json:"units"`
	Orders   int     `json:"orders"`
	Revenue  float64 `json:"revenue"`
}

type MarketplaceSalesTotalDTO struct {
	Platform string  `json:"platform,omitempty"`
	Units    int64   `json:"units"`
	Orders   int64   `json:"orders"`
	Revenue  float64 `json:"revenue"`
}

type MarketplaceSalesDTO struct {
	Days       int                        `json:"days"`
	From       string                     `json:"from"`
	To         string                     `json:"to"`
	Total      MarketplaceSalesTotalDTO   `json:"total"`
	ByPlatform []MarketplaceSalesTotalDTO `json:"byPlatform"`
	Rows       []MarketplaceSaleDTO       `json:"rows"`
}

type MarketplaceAdAngleDTO struct {
	Format  string   `json:"format"`
	Fit     string   `json:"fit"`
	Title   string   `json:"title"`
	Why     string   `json:"why"`
	Hook    string   `json:"hook"`
	Outline []string `json:"outline"`
}

type MarketplaceCaptionDTO struct {
	Platform string `json:"platform"`
	Caption  string `json:"caption"`
}

// MarketplaceSuggestionDTO is model output, stored verbatim after sanitising.
type MarketplaceSuggestionDTO struct {
	SimilarProducts []string                `json:"similarProducts"`
	Angles          []MarketplaceAdAngleDTO `json:"angles"`
	Captions        []MarketplaceCaptionDTO `json:"captions"`
	Hashtags        []string                `json:"hashtags"`
	GeneratedAt     string                  `json:"generatedAt"`
}

type MarketplaceTrendItemDTO struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Why         string `json:"why"`
	ContentIdea string `json:"contentIdea"`
}

// MarketplaceTrendsDTO comes from the model's general knowledge, not live marketplace data.
type MarketplaceTrendsDTO struct {
	Items       []MarketplaceTrendItemDTO `json:"items"`
	GeneratedAt string                    `json:"generatedAt"`
}
