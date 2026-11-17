package models

import (
	"time"

	"github.com/google/uuid"
)

type MarketplaceShop struct {
	Base
	UserID         uuid.UUID  `gorm:"type:uuid;not null;index"`
	Name           string     `gorm:"not null"`
	Description    string     `gorm:"not null;default:''"`
	Category       string     `gorm:"not null;default:''"`
	LogoUploadID   *uuid.UUID `gorm:"column:logo_upload_id;type:uuid"`
	LogoKey        *string    `gorm:"column:logo_key"`
	Phone          string     `gorm:"not null;default:''"`
	Email          string     `gorm:"not null;default:''"`
	LineID         string     `gorm:"column:line_id;not null;default:''"`
	Website        string     `gorm:"not null;default:''"`
	Address        string     `gorm:"not null;default:''"`
	BrandTone      string     `gorm:"column:brand_tone;not null;default:''"`
	TargetAudience string     `gorm:"column:target_audience;not null;default:''"`
	TiktokShopURL  string     `gorm:"column:tiktok_shop_url;not null;default:''"`
	ShopeeURL      string     `gorm:"column:shopee_url;not null;default:''"`
	LazadaURL      string     `gorm:"column:lazada_url;not null;default:''"`
	Status         string     `gorm:"not null;default:'active'"`
	AITrends       []byte     `gorm:"column:ai_trends;type:jsonb"`
	AITrendsAt     *time.Time `gorm:"column:ai_trends_at"`
}

func (MarketplaceShop) TableName() string { return "marketplace_shops" }

type MarketplaceProduct struct {
	Base
	UserID        uuid.UUID `gorm:"type:uuid;not null;index"`
	ShopID        uuid.UUID `gorm:"type:uuid;not null;index"`
	Name          string    `gorm:"not null"`
	Description   string    `gorm:"not null;default:''"`
	SellingPoints string    `gorm:"column:selling_points;not null;default:''"`
	Category      string    `gorm:"not null;default:''"`
	SKU           string    `gorm:"column:sku;not null;default:''"`
	// Satang (1/100 baht).
	PriceMinor    int64      `gorm:"column:price_minor;not null;default:0"`
	Stock         int        `gorm:"not null;default:0"`
	Status        string     `gorm:"not null;default:'active'"`
	Images        []byte     `gorm:"type:jsonb;not null"`
	TiktokShopURL string     `gorm:"column:tiktok_shop_url;not null;default:''"`
	ShopeeURL     string     `gorm:"column:shopee_url;not null;default:''"`
	LazadaURL     string     `gorm:"column:lazada_url;not null;default:''"`
	AISuggestion  []byte     `gorm:"column:ai_suggestion;type:jsonb"`
	AISuggestedAt *time.Time `gorm:"column:ai_suggested_at"`
}

func (MarketplaceProduct) TableName() string { return "marketplace_products" }

type MarketplaceContent struct {
	Base
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index"`
	ProductID uuid.UUID  `gorm:"type:uuid;not null;index"`
	Kind      string     `gorm:"not null"`
	Title     string     `gorm:"not null"`
	Brief     string     `gorm:"not null;default:''"`
	RefType   string     `gorm:"column:ref_type;not null;default:''"`
	RefID     *uuid.UUID `gorm:"column:ref_id;type:uuid"`
}

func (MarketplaceContent) TableName() string { return "marketplace_contents" }

type MarketplacePublish struct {
	Base
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index"`
	ProductID uuid.UUID  `gorm:"type:uuid;not null;index"`
	ContentID *uuid.UUID `gorm:"column:content_id;type:uuid"`
	Platform  string     `gorm:"not null"`
	Caption   string     `gorm:"not null;default:''"`
	PostURL   string     `gorm:"column:post_url;not null;default:''"`
	PostedOn  time.Time  `gorm:"column:posted_on;type:date;not null"`
}

func (MarketplacePublish) TableName() string { return "marketplace_publishes" }

// MarketplaceSale has no soft delete: (product_id, platform, sale_date) is unique and re-used.
type MarketplaceSale struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID       uuid.UUID `gorm:"type:uuid;not null"`
	ProductID    uuid.UUID `gorm:"type:uuid;not null"`
	Platform     string    `gorm:"not null"`
	SaleDate     time.Time `gorm:"column:sale_date;type:date;not null"`
	Units        int       `gorm:"not null;default:0"`
	Orders       int       `gorm:"not null;default:0"`
	RevenueMinor int64     `gorm:"column:revenue_minor;not null;default:0"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (MarketplaceSale) TableName() string { return "marketplace_sales" }
