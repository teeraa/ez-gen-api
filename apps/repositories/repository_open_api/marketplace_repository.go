package repository_open_api

import (
	"context"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MarketplaceShopRow struct {
	models.MarketplaceShop
	ProductCount int64 `gorm:"column:product_count"`
}

// MarketplaceProductRow carries the list-card figures: sales since the filter's SalesSince date.
type MarketplaceProductRow struct {
	models.MarketplaceProduct
	RecentUnits        int64 `gorm:"column:recent_units"`
	RecentRevenueMinor int64 `gorm:"column:recent_revenue_minor"`
	ContentCount       int64 `gorm:"column:content_count"`
}

type MarketplaceProductFilter struct {
	Query      string
	Status     string
	SalesSince time.Time
	Limit      int
	Offset     int
}

type MarketplaceShopFields struct {
	Name, Description, Category                 string
	LogoUploadID                                *uuid.UUID
	LogoKey                                     *string
	Phone, Email, LineID, Website, Address      string
	BrandTone, TargetAudience                   string
	TiktokShopURL, ShopeeURL, LazadaURL, Status string
}

type MarketplaceProductFields struct {
	Name, Description, SellingPoints, Category, SKU string
	PriceMinor                                      int64
	Stock                                           int
	Status                                          string
	Images                                          []byte
	TiktokShopURL, ShopeeURL, LazadaURL             string
}

// Every method that takes a userID only touches rows owned by that user.
type MarketplaceRepository interface {
	ListShops(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]MarketplaceShopRow, int64, error)
	FindShop(ctx context.Context, id, userID uuid.UUID) (*MarketplaceShopRow, error)
	CountShops(ctx context.Context, userID uuid.UUID) (int64, error)
	CreateShop(ctx context.Context, s *models.MarketplaceShop) error
	UpdateShop(ctx context.Context, id, userID uuid.UUID, f MarketplaceShopFields) error
	DeleteShop(ctx context.Context, id, userID uuid.UUID) error
	SetShopTrends(ctx context.Context, id, userID uuid.UUID, data []byte, at time.Time) error

	ListProducts(ctx context.Context, shopID, userID uuid.UUID, f MarketplaceProductFilter) ([]MarketplaceProductRow, int64, error)
	FindProduct(ctx context.Context, id, userID uuid.UUID) (*models.MarketplaceProduct, error)
	CountProducts(ctx context.Context, shopID, userID uuid.UUID) (int64, error)
	CreateProduct(ctx context.Context, p *models.MarketplaceProduct) error
	UpdateProduct(ctx context.Context, id, userID uuid.UUID, f MarketplaceProductFields) error
	DeleteProduct(ctx context.Context, id, userID uuid.UUID) error
	SetProductSuggestion(ctx context.Context, id, userID uuid.UUID, data []byte, at time.Time) error

	ListContents(ctx context.Context, productID, userID uuid.UUID) ([]models.MarketplaceContent, error)
	FindContent(ctx context.Context, id, userID uuid.UUID) (*models.MarketplaceContent, error)
	CreateContent(ctx context.Context, c *models.MarketplaceContent) error
	DeleteContent(ctx context.Context, id, userID uuid.UUID) error

	ListPublishes(ctx context.Context, productID, userID uuid.UUID) ([]models.MarketplacePublish, error)
	CreatePublish(ctx context.Context, p *models.MarketplacePublish) error
	DeletePublish(ctx context.Context, id, userID uuid.UUID) error

	UpsertSales(ctx context.Context, rows []models.MarketplaceSale) error
	ListSales(ctx context.Context, productID, userID uuid.UUID, since time.Time) ([]models.MarketplaceSale, error)
	DeleteSale(ctx context.Context, id, userID uuid.UUID) error
}

type marketplaceRepository struct{ db *gorm.DB }

func NewMarketplaceRepository(db *gorm.DB) MarketplaceRepository {
	return &marketplaceRepository{db: db}
}

// affected turns "no row matched the owner-scoped predicate" into ErrNotFound.
func affected(op string, res *gorm.DB) error {
	if res.Error != nil {
		return wrapErr(op, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Shops ─────────────────────────────────────────────────────────────────────

const marketplaceShopSelect = `SELECT s.*, (SELECT count(*) FROM marketplace_products p WHERE p.shop_id = s.id AND p.deleted_at IS NULL) AS product_count FROM marketplace_shops s WHERE `

func (r *marketplaceRepository) ListShops(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]MarketplaceShopRow, int64, error) {
	where := []string{"s.user_id = ?", "s.deleted_at IS NULL"}
	args := []any{userID}
	if query != "" {
		where = append(where, "(s.name ILIKE ? OR s.category ILIKE ?)")
		pat := likePattern(query)
		args = append(args, pat, pat)
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Raw("SELECT count(*) FROM marketplace_shops s WHERE "+cond, args...).Scan(&total).Error; err != nil {
		return nil, 0, wrapErr("count shops", err)
	}
	rows := []MarketplaceShopRow{}
	if total == 0 {
		return rows, 0, nil
	}
	args = append(args, limit, offset)
	if err := r.db.WithContext(ctx).Raw(marketplaceShopSelect+cond+" ORDER BY s.created_at DESC, s.id LIMIT ? OFFSET ?", args...).Scan(&rows).Error; err != nil {
		return nil, 0, wrapErr("list shops", err)
	}
	return rows, total, nil
}

func (r *marketplaceRepository) FindShop(ctx context.Context, id, userID uuid.UUID) (*MarketplaceShopRow, error) {
	rows := []MarketplaceShopRow{}
	if err := r.db.WithContext(ctx).Raw(marketplaceShopSelect+"s.id = ? AND s.user_id = ? AND s.deleted_at IS NULL", id, userID).Scan(&rows).Error; err != nil {
		return nil, wrapErr("find shop", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

func (r *marketplaceRepository) CountShops(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.MarketplaceShop{}).Where("user_id = ?", userID).Count(&n).Error
	return n, wrapErr("count shops", err)
}

func (r *marketplaceRepository) CreateShop(ctx context.Context, s *models.MarketplaceShop) error {
	return wrapErr("create shop", r.db.WithContext(ctx).Create(s).Error)
}

func (r *marketplaceRepository) UpdateShop(ctx context.Context, id, userID uuid.UUID, f MarketplaceShopFields) error {
	res := r.db.WithContext(ctx).Model(&models.MarketplaceShop{}).Where("id = ? AND user_id = ?", id, userID).Updates(map[string]any{
		"name": f.Name, "description": f.Description, "category": f.Category,
		"logo_upload_id": f.LogoUploadID, "logo_key": f.LogoKey,
		"phone": f.Phone, "email": f.Email, "line_id": f.LineID, "website": f.Website, "address": f.Address,
		"brand_tone": f.BrandTone, "target_audience": f.TargetAudience,
		"tiktok_shop_url": f.TiktokShopURL, "shopee_url": f.ShopeeURL, "lazada_url": f.LazadaURL,
		"status": f.Status,
	})
	return affected("update shop", res)
}

// DeleteShop also removes the shop's products so they stop counting against limits and lists.
func (r *marketplaceRepository) DeleteShop(ctx context.Context, id, userID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := affected("delete shop", tx.Where("id = ? AND user_id = ?", id, userID).Delete(&models.MarketplaceShop{})); err != nil {
			return err
		}
		return wrapErr("delete shop products", tx.Where("shop_id = ? AND user_id = ?", id, userID).Delete(&models.MarketplaceProduct{}).Error)
	})
}

func (r *marketplaceRepository) SetShopTrends(ctx context.Context, id, userID uuid.UUID, data []byte, at time.Time) error {
	res := r.db.WithContext(ctx).Exec(`UPDATE marketplace_shops SET ai_trends = ?::jsonb, ai_trends_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, string(data), at, id, userID)
	return affected("set shop trends", res)
}

// ── Products ──────────────────────────────────────────────────────────────────

func (r *marketplaceRepository) ListProducts(ctx context.Context, shopID, userID uuid.UUID, f MarketplaceProductFilter) ([]MarketplaceProductRow, int64, error) {
	where := []string{"p.shop_id = ?", "p.user_id = ?", "p.deleted_at IS NULL"}
	args := []any{shopID, userID}
	if f.Status != "" {
		where = append(where, "p.status = ?")
		args = append(args, f.Status)
	}
	if f.Query != "" {
		where = append(where, "(p.name ILIKE ? OR p.sku ILIKE ? OR p.category ILIKE ?)")
		pat := likePattern(f.Query)
		args = append(args, pat, pat, pat)
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Raw("SELECT count(*) FROM marketplace_products p WHERE "+cond, args...).Scan(&total).Error; err != nil {
		return nil, 0, wrapErr("count products", err)
	}
	rows := []MarketplaceProductRow{}
	if total == 0 {
		return rows, 0, nil
	}
	const sel = `SELECT p.*,
  COALESCE((SELECT sum(x.units) FROM marketplace_sales x WHERE x.product_id = p.id AND x.sale_date >= ?::date), 0) AS recent_units,
  COALESCE((SELECT sum(x.revenue_minor) FROM marketplace_sales x WHERE x.product_id = p.id AND x.sale_date >= ?::date), 0) AS recent_revenue_minor,
  (SELECT count(*) FROM marketplace_contents c WHERE c.product_id = p.id AND c.deleted_at IS NULL) AS content_count
FROM marketplace_products p WHERE `
	since := f.SalesSince.Format("2006-01-02")
	full := append([]any{since, since}, args...)
	full = append(full, f.Limit, f.Offset)
	if err := r.db.WithContext(ctx).Raw(sel+cond+" ORDER BY p.created_at DESC, p.id LIMIT ? OFFSET ?", full...).Scan(&rows).Error; err != nil {
		return nil, 0, wrapErr("list products", err)
	}
	return rows, total, nil
}

func (r *marketplaceRepository) FindProduct(ctx context.Context, id, userID uuid.UUID) (*models.MarketplaceProduct, error) {
	var p models.MarketplaceProduct
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&p).Error; err != nil {
		return nil, wrapErr("find product", err)
	}
	return &p, nil
}

func (r *marketplaceRepository) CountProducts(ctx context.Context, shopID, userID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.MarketplaceProduct{}).Where("shop_id = ? AND user_id = ?", shopID, userID).Count(&n).Error
	return n, wrapErr("count products", err)
}

func (r *marketplaceRepository) CreateProduct(ctx context.Context, p *models.MarketplaceProduct) error {
	return wrapErr("create product", r.db.WithContext(ctx).Create(p).Error)
}

func (r *marketplaceRepository) UpdateProduct(ctx context.Context, id, userID uuid.UUID, f MarketplaceProductFields) error {
	res := r.db.WithContext(ctx).Model(&models.MarketplaceProduct{}).Where("id = ? AND user_id = ?", id, userID).Updates(map[string]any{
		"name": f.Name, "description": f.Description, "selling_points": f.SellingPoints, "category": f.Category, "sku": f.SKU,
		"price_minor": f.PriceMinor, "stock": f.Stock, "status": f.Status, "images": f.Images,
		"tiktok_shop_url": f.TiktokShopURL, "shopee_url": f.ShopeeURL, "lazada_url": f.LazadaURL,
	})
	return affected("update product", res)
}

func (r *marketplaceRepository) DeleteProduct(ctx context.Context, id, userID uuid.UUID) error {
	return affected("delete product", r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.MarketplaceProduct{}))
}

func (r *marketplaceRepository) SetProductSuggestion(ctx context.Context, id, userID uuid.UUID, data []byte, at time.Time) error {
	res := r.db.WithContext(ctx).Exec(`UPDATE marketplace_products SET ai_suggestion = ?::jsonb, ai_suggested_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, string(data), at, id, userID)
	return affected("set product suggestion", res)
}

// ── Contents ──────────────────────────────────────────────────────────────────

func (r *marketplaceRepository) ListContents(ctx context.Context, productID, userID uuid.UUID) ([]models.MarketplaceContent, error) {
	rows := []models.MarketplaceContent{}
	err := r.db.WithContext(ctx).Where("product_id = ? AND user_id = ?", productID, userID).Order("created_at DESC, id").Limit(200).Find(&rows).Error
	return rows, wrapErr("list contents", err)
}

func (r *marketplaceRepository) FindContent(ctx context.Context, id, userID uuid.UUID) (*models.MarketplaceContent, error) {
	var c models.MarketplaceContent
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&c).Error; err != nil {
		return nil, wrapErr("find content", err)
	}
	return &c, nil
}

func (r *marketplaceRepository) CreateContent(ctx context.Context, c *models.MarketplaceContent) error {
	return wrapErr("create content", r.db.WithContext(ctx).Create(c).Error)
}

func (r *marketplaceRepository) DeleteContent(ctx context.Context, id, userID uuid.UUID) error {
	return affected("delete content", r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.MarketplaceContent{}))
}

// ── Publishes ─────────────────────────────────────────────────────────────────

func (r *marketplaceRepository) ListPublishes(ctx context.Context, productID, userID uuid.UUID) ([]models.MarketplacePublish, error) {
	rows := []models.MarketplacePublish{}
	err := r.db.WithContext(ctx).Where("product_id = ? AND user_id = ?", productID, userID).Order("posted_on DESC, created_at DESC, id").Limit(200).Find(&rows).Error
	return rows, wrapErr("list publishes", err)
}

func (r *marketplaceRepository) CreatePublish(ctx context.Context, p *models.MarketplacePublish) error {
	return wrapErr("create publish", r.db.WithContext(ctx).Create(p).Error)
}

func (r *marketplaceRepository) DeletePublish(ctx context.Context, id, userID uuid.UUID) error {
	return affected("delete publish", r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.MarketplacePublish{}))
}

// ── Sales ─────────────────────────────────────────────────────────────────────

func (r *marketplaceRepository) UpsertSales(ctx context.Context, rows []models.MarketplaceSale) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, s := range rows {
			err := tx.Exec(`INSERT INTO marketplace_sales (id, user_id, product_id, platform, sale_date, units, orders, revenue_minor)
VALUES (?, ?, ?, ?, ?::date, ?, ?, ?)
ON CONFLICT (product_id, platform, sale_date) DO UPDATE
SET units = EXCLUDED.units, orders = EXCLUDED.orders, revenue_minor = EXCLUDED.revenue_minor, updated_at = now()`,
				uuid.New(), s.UserID, s.ProductID, s.Platform, s.SaleDate.Format("2006-01-02"), s.Units, s.Orders, s.RevenueMinor).Error
			if err != nil {
				return wrapErr("upsert sale", err)
			}
		}
		return nil
	})
}

func (r *marketplaceRepository) ListSales(ctx context.Context, productID, userID uuid.UUID, since time.Time) ([]models.MarketplaceSale, error) {
	rows := []models.MarketplaceSale{}
	err := r.db.WithContext(ctx).Where("product_id = ? AND user_id = ? AND sale_date >= ?::date", productID, userID, since.Format("2006-01-02")).
		Order("sale_date DESC, platform").Limit(1200).Find(&rows).Error
	return rows, wrapErr("list sales", err)
}

func (r *marketplaceRepository) DeleteSale(ctx context.Context, id, userID uuid.UUID) error {
	return affected("delete sale", r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.MarketplaceSale{}))
}
