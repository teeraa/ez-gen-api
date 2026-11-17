package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

var (
	ErrMarketplaceLimit         = errors.New("marketplace item limit reached")
	ErrMarketplaceAIUnavailable = errors.New("marketplace ai is not configured")
	ErrMarketplaceAIFailed      = errors.New("marketplace ai request failed")
)

// Per-user caps keep a single account from growing the shared tables without bound.
const (
	maxMarketplaceShops     = 50
	maxMarketplaceProducts  = 500
	maxMarketplaceImages    = 6
	maxMarketplaceContents  = 100
	maxMarketplacePublishes = 200
	maxMarketplaceSaleRows  = 400
	marketplaceRecentDays   = 30
	marketplaceSignTTL      = time.Hour
	marketplaceDateShape    = "2006-01-02"
)

var (
	marketplacePlatforms = []string{"tiktok_shop", "shopee", "lazada"}
	// Sales dates are calendar days as the seller sees them, so "today" is Thai time.
	marketplaceZone     = time.FixedZone("ICT", 7*3600)
	marketplaceEarliest = time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
)

// MarketplaceAnalyzer is the text model used for ad-angle and trend suggestions.
type MarketplaceAnalyzer interface {
	Analyze(ctx context.Context, model, prompt string) (string, error)
}

type marketplaceUploads interface {
	FindByID(ctx context.Context, id, userID string) (*models.Upload, error)
}

type marketplaceStorage interface {
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
}

type MarketplaceOptions struct {
	Analyzer      MarketplaceAnalyzer
	AnalysisModel string
}

type MarketplaceShopInput struct {
	Name, Description, Category            string
	LogoUploadID                           *string
	Phone, Email, LineID, Website, Address string
	BrandTone, TargetAudience              string
	TiktokShopURL, ShopeeURL, LazadaURL    string
	Status                                 string
}

type MarketplaceProductInput struct {
	Name, Description, SellingPoints, Category, SKU string
	Price                                           float64
	Stock                                           int
	Status                                          string
	ImageUploadIDs                                  []string
	TiktokShopURL, ShopeeURL, LazadaURL             string
}

type MarketplaceContentInput struct {
	Kind, Title, Brief, RefType string
	RefID                       *string
}

type MarketplacePublishInput struct {
	ContentID                            *string
	Platform, Caption, PostURL, PostedOn string
}

type MarketplaceSaleInput struct {
	Platform, Date string
	Units, Orders  int
	Revenue        float64
}

type MarketplaceListQuery struct {
	Query  string
	Status string
	Page   int
	Limit  int
}

// MarketplaceFacade serves the Marketplace menu. This phase is manual: posting to TikTok Shop,
// Shopee and Lazada happens outside the app and is only logged here, and sales are typed in or imported.
type MarketplaceFacade struct {
	repo    repository_open_api.MarketplaceRepository
	uploads marketplaceUploads
	storage marketplaceStorage
	options MarketplaceOptions
	now     func() time.Time
}

func NewMarketplaceFacade(repo repository_open_api.MarketplaceRepository, uploads marketplaceUploads, storage marketplaceStorage, options MarketplaceOptions) *MarketplaceFacade {
	return &MarketplaceFacade{repo: repo, uploads: uploads, storage: storage, options: options, now: time.Now}
}

type marketplaceImage struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func marketplacePlatform(p string) bool {
	for _, known := range marketplacePlatforms {
		if p == known {
			return true
		}
	}
	return false
}

// marketplaceURL accepts only http(s) links: these values are rendered as hrefs.
func marketplaceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || utf8.RuneCountInString(raw) > 500 {
		return "", ErrValidation
	}
	return u.String(), nil
}

func marketplaceMinor(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || amount > 1e11 {
		return 0, ErrValidation
	}
	return int64(math.Round(amount * 100)), nil
}

func marketplaceBaht(minor int64) float64 { return float64(minor) / 100 }

func (f *MarketplaceFacade) today() time.Time {
	n := f.now().In(marketplaceZone)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// day parses a calendar date and rejects ones outside the plausible range for a sale or post.
func (f *MarketplaceFacade) day(raw string) (time.Time, error) {
	d, err := time.Parse(marketplaceDateShape, raw)
	if err != nil || d.Before(marketplaceEarliest) || d.After(f.today().AddDate(0, 0, 1)) {
		return time.Time{}, ErrValidation
	}
	return d, nil
}

// signAll signs keys concurrently; an empty key or a failed signature yields "".
func (f *MarketplaceFacade) signAll(ctx context.Context, keys []string) []string {
	out := make([]string, len(keys))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, key := range keys {
		if key == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			if u, _, err := f.storage.SignedURL(ctx, key, marketplaceSignTTL); err == nil {
				out[i] = u
			}
		}(i, key)
	}
	wg.Wait()
	return out
}

// imageUpload resolves an upload the user owns into its storage key, which outlives the upload row.
func (f *MarketplaceFacade) imageUpload(ctx context.Context, id, userID string) (string, error) {
	up, err := f.uploads.FindByID(ctx, id, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return "", ErrValidation
		}
		return "", fmt.Errorf("find upload: %w", err)
	}
	if !strings.HasPrefix(up.MimeType, "image/") {
		return "", ErrInvalidFileType
	}
	return up.StorageKey, nil
}

// ── Shops ─────────────────────────────────────────────────────────────────────

func (f *MarketplaceFacade) shopFields(ctx context.Context, userID string, in MarketplaceShopInput, current *models.MarketplaceShop) (repository_open_api.MarketplaceShopFields, error) {
	out := repository_open_api.MarketplaceShopFields{
		Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), Category: strings.TrimSpace(in.Category),
		Phone: strings.TrimSpace(in.Phone), Email: strings.TrimSpace(in.Email), LineID: strings.TrimSpace(in.LineID), Address: strings.TrimSpace(in.Address),
		BrandTone: strings.TrimSpace(in.BrandTone), TargetAudience: strings.TrimSpace(in.TargetAudience), Status: in.Status,
	}
	if out.Name == "" {
		return out, ErrValidation
	}
	if out.Status == "" {
		out.Status = "active"
	}
	var err error
	for _, link := range []struct {
		dst *string
		src string
	}{{&out.Website, in.Website}, {&out.TiktokShopURL, in.TiktokShopURL}, {&out.ShopeeURL, in.ShopeeURL}, {&out.LazadaURL, in.LazadaURL}} {
		if *link.dst, err = marketplaceURL(link.src); err != nil {
			return out, err
		}
	}
	if in.LogoUploadID == nil || *in.LogoUploadID == "" {
		return out, nil
	}
	logoID, err := uuid.Parse(*in.LogoUploadID)
	if err != nil {
		return out, ErrValidation
	}
	out.LogoUploadID = &logoID
	if current != nil && current.LogoUploadID != nil && *current.LogoUploadID == logoID && current.LogoKey != nil {
		out.LogoKey = current.LogoKey
		return out, nil
	}
	key, err := f.imageUpload(ctx, logoID.String(), userID)
	if err != nil {
		return out, err
	}
	out.LogoKey = &key
	return out, nil
}

func marketplaceShopDTO(r repository_open_api.MarketplaceShopRow, logoURL string) dto_open_api.MarketplaceShopDTO {
	d := dto_open_api.MarketplaceShopDTO{
		ID: r.ID.String(), Name: r.Name, Description: r.Description, Category: r.Category,
		Phone: r.Phone, Email: r.Email, LineID: r.LineID, Website: r.Website, Address: r.Address,
		BrandTone: r.BrandTone, TargetAudience: r.TargetAudience,
		TiktokShopURL: r.TiktokShopURL, ShopeeURL: r.ShopeeURL, LazadaURL: r.LazadaURL,
		Status: r.Status, ProductCount: r.ProductCount,
		CreatedAt: libraryTime(r.CreatedAt), UpdatedAt: libraryTime(r.UpdatedAt),
	}
	if r.LogoUploadID != nil {
		id := r.LogoUploadID.String()
		d.LogoUploadID = &id
	}
	if logoURL != "" {
		d.LogoURL = &logoURL
	}
	return d
}

func (f *MarketplaceFacade) ListShops(ctx context.Context, userID string, q MarketplaceListQuery) (*dto_open_api.MarketplaceShopListDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	rows, total, err := f.repo.ListShops(ctx, uid, strings.TrimSpace(q.Query), q.Limit, (q.Page-1)*q.Limit)
	if err != nil {
		return nil, fmt.Errorf("list shops: %w", err)
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		if r.LogoKey != nil {
			keys[i] = *r.LogoKey
		}
	}
	urls := f.signAll(ctx, keys)
	items := make([]dto_open_api.MarketplaceShopDTO, 0, len(rows))
	for i, r := range rows {
		items = append(items, marketplaceShopDTO(r, urls[i]))
	}
	return &dto_open_api.MarketplaceShopListDTO{Items: items, Total: total, Page: q.Page, Limit: q.Limit}, nil
}

func (f *MarketplaceFacade) GetShop(ctx context.Context, id, userID string) (*dto_open_api.MarketplaceShopDTO, error) {
	sid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	row, err := f.repo.FindShop(ctx, sid, uid)
	if err != nil {
		return nil, libraryRepoErr("find shop", err)
	}
	key := ""
	if row.LogoKey != nil {
		key = *row.LogoKey
	}
	d := marketplaceShopDTO(*row, f.signAll(ctx, []string{key})[0])
	if len(row.AITrends) > 0 {
		var t dto_open_api.MarketplaceTrendsDTO
		if json.Unmarshal(row.AITrends, &t) == nil && len(t.Items) > 0 {
			d.Trends = &t
		}
	}
	return &d, nil
}

func (f *MarketplaceFacade) CreateShop(ctx context.Context, userID string, in MarketplaceShopInput) (*dto_open_api.MarketplaceShopDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	fields, err := f.shopFields(ctx, userID, in, nil)
	if err != nil {
		return nil, err
	}
	n, err := f.repo.CountShops(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("count shops: %w", err)
	}
	if n >= maxMarketplaceShops {
		return nil, ErrMarketplaceLimit
	}
	shop := &models.MarketplaceShop{
		UserID: uid, Name: fields.Name, Description: fields.Description, Category: fields.Category,
		LogoUploadID: fields.LogoUploadID, LogoKey: fields.LogoKey,
		Phone: fields.Phone, Email: fields.Email, LineID: fields.LineID, Website: fields.Website, Address: fields.Address,
		BrandTone: fields.BrandTone, TargetAudience: fields.TargetAudience,
		TiktokShopURL: fields.TiktokShopURL, ShopeeURL: fields.ShopeeURL, LazadaURL: fields.LazadaURL, Status: fields.Status,
	}
	if err := f.repo.CreateShop(ctx, shop); err != nil {
		return nil, fmt.Errorf("create shop: %w", err)
	}
	return f.GetShop(ctx, shop.ID.String(), userID)
}

func (f *MarketplaceFacade) UpdateShop(ctx context.Context, id, userID string, in MarketplaceShopInput) (*dto_open_api.MarketplaceShopDTO, error) {
	sid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	current, err := f.repo.FindShop(ctx, sid, uid)
	if err != nil {
		return nil, libraryRepoErr("find shop", err)
	}
	fields, err := f.shopFields(ctx, userID, in, &current.MarketplaceShop)
	if err != nil {
		return nil, err
	}
	if err := f.repo.UpdateShop(ctx, sid, uid, fields); err != nil {
		return nil, libraryRepoErr("update shop", err)
	}
	return f.GetShop(ctx, id, userID)
}

func (f *MarketplaceFacade) DeleteShop(ctx context.Context, id, userID string) error {
	sid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	return libraryRepoErr("delete shop", f.repo.DeleteShop(ctx, sid, uid))
}

// ── Products ──────────────────────────────────────────────────────────────────

func marketplaceImages(raw []byte) []marketplaceImage {
	var images []marketplaceImage
	if json.Unmarshal(raw, &images) != nil {
		return nil
	}
	return images
}

func (f *MarketplaceFacade) productFields(ctx context.Context, userID string, in MarketplaceProductInput, current []marketplaceImage) (repository_open_api.MarketplaceProductFields, error) {
	out := repository_open_api.MarketplaceProductFields{
		Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), SellingPoints: strings.TrimSpace(in.SellingPoints),
		Category: strings.TrimSpace(in.Category), SKU: strings.TrimSpace(in.SKU), Stock: in.Stock, Status: in.Status,
	}
	if out.Name == "" || in.Stock < 0 || len(in.ImageUploadIDs) > maxMarketplaceImages {
		return out, ErrValidation
	}
	if out.Status == "" {
		out.Status = "active"
	}
	var err error
	if out.PriceMinor, err = marketplaceMinor(in.Price); err != nil {
		return out, err
	}
	for _, link := range []struct {
		dst *string
		src string
	}{{&out.TiktokShopURL, in.TiktokShopURL}, {&out.ShopeeURL, in.ShopeeURL}, {&out.LazadaURL, in.LazadaURL}} {
		if *link.dst, err = marketplaceURL(link.src); err != nil {
			return out, err
		}
	}
	kept := map[string]string{}
	for _, img := range current {
		kept[img.ID] = img.Key
	}
	images := make([]marketplaceImage, 0, len(in.ImageUploadIDs))
	seen := map[string]bool{}
	for _, raw := range in.ImageUploadIDs {
		id, err := uuid.Parse(raw)
		if err != nil || seen[id.String()] {
			return out, ErrValidation
		}
		seen[id.String()] = true
		key, ok := kept[id.String()]
		if !ok {
			if key, err = f.imageUpload(ctx, id.String(), userID); err != nil {
				return out, err
			}
		}
		images = append(images, marketplaceImage{ID: id.String(), Key: key})
	}
	if out.Images, err = json.Marshal(images); err != nil {
		return out, fmt.Errorf("encode images: %w", err)
	}
	return out, nil
}

func marketplaceProductDTO(p models.MarketplaceProduct, images []dto_open_api.MarketplaceImageDTO) dto_open_api.MarketplaceProductDTO {
	return dto_open_api.MarketplaceProductDTO{
		ID: p.ID.String(), ShopID: p.ShopID.String(), Name: p.Name, Description: p.Description, SellingPoints: p.SellingPoints,
		Category: p.Category, SKU: p.SKU, Price: marketplaceBaht(p.PriceMinor), Stock: p.Stock, Status: p.Status, Images: images,
		TiktokShopURL: p.TiktokShopURL, ShopeeURL: p.ShopeeURL, LazadaURL: p.LazadaURL,
		CreatedAt: libraryTime(p.CreatedAt), UpdatedAt: libraryTime(p.UpdatedAt),
	}
}

func (f *MarketplaceFacade) ListProducts(ctx context.Context, shopID, userID string, q MarketplaceListQuery) (*dto_open_api.MarketplaceProductListDTO, error) {
	sid, uid, err := libraryIDs(shopID, userID)
	if err != nil {
		return nil, err
	}
	if _, err := f.repo.FindShop(ctx, sid, uid); err != nil {
		return nil, libraryRepoErr("find shop", err)
	}
	rows, total, err := f.repo.ListProducts(ctx, sid, uid, repository_open_api.MarketplaceProductFilter{
		Query: strings.TrimSpace(q.Query), Status: q.Status, SalesSince: f.today().AddDate(0, 0, -(marketplaceRecentDays - 1)),
		Limit: q.Limit, Offset: (q.Page - 1) * q.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	// Cards show one picture, so only the cover is signed.
	covers := make([]marketplaceImage, len(rows))
	keys := make([]string, len(rows))
	for i, r := range rows {
		if images := marketplaceImages(r.Images); len(images) > 0 {
			covers[i], keys[i] = images[0], images[0].Key
		}
	}
	urls := f.signAll(ctx, keys)
	items := make([]dto_open_api.MarketplaceProductDTO, 0, len(rows))
	for i, r := range rows {
		images := []dto_open_api.MarketplaceImageDTO{}
		if urls[i] != "" {
			images = append(images, dto_open_api.MarketplaceImageDTO{ID: covers[i].ID, URL: urls[i]})
		}
		d := marketplaceProductDTO(r.MarketplaceProduct, images)
		d.RecentUnits, d.RecentRevenue, d.ContentCount = r.RecentUnits, marketplaceBaht(r.RecentRevenueMinor), r.ContentCount
		items = append(items, d)
	}
	return &dto_open_api.MarketplaceProductListDTO{Items: items, Total: total, Page: q.Page, Limit: q.Limit}, nil
}

func (f *MarketplaceFacade) product(ctx context.Context, id, userID string) (*models.MarketplaceProduct, uuid.UUID, error) {
	pid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	p, err := f.repo.FindProduct(ctx, pid, uid)
	if err != nil {
		return nil, uuid.Nil, libraryRepoErr("find product", err)
	}
	return p, uid, nil
}

func (f *MarketplaceFacade) GetProduct(ctx context.Context, id, userID string) (*dto_open_api.MarketplaceProductDTO, error) {
	p, uid, err := f.product(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	stored := marketplaceImages(p.Images)
	keys := make([]string, len(stored))
	for i, img := range stored {
		keys[i] = img.Key
	}
	urls := f.signAll(ctx, keys)
	images := make([]dto_open_api.MarketplaceImageDTO, 0, len(stored))
	for i, img := range stored {
		if urls[i] != "" {
			images = append(images, dto_open_api.MarketplaceImageDTO{ID: img.ID, URL: urls[i]})
		}
	}
	d := marketplaceProductDTO(*p, images)
	if shop, err := f.repo.FindShop(ctx, p.ShopID, uid); err == nil {
		d.ShopName = shop.Name
	}
	if len(p.AISuggestion) > 0 {
		var s dto_open_api.MarketplaceSuggestionDTO
		if json.Unmarshal(p.AISuggestion, &s) == nil && len(s.Angles) > 0 {
			d.Suggestion = &s
		}
	}
	return &d, nil
}

func (f *MarketplaceFacade) CreateProduct(ctx context.Context, shopID, userID string, in MarketplaceProductInput) (*dto_open_api.MarketplaceProductDTO, error) {
	sid, uid, err := libraryIDs(shopID, userID)
	if err != nil {
		return nil, err
	}
	if _, err := f.repo.FindShop(ctx, sid, uid); err != nil {
		return nil, libraryRepoErr("find shop", err)
	}
	fields, err := f.productFields(ctx, userID, in, nil)
	if err != nil {
		return nil, err
	}
	n, err := f.repo.CountProducts(ctx, sid, uid)
	if err != nil {
		return nil, fmt.Errorf("count products: %w", err)
	}
	if n >= maxMarketplaceProducts {
		return nil, ErrMarketplaceLimit
	}
	p := &models.MarketplaceProduct{
		UserID: uid, ShopID: sid, Name: fields.Name, Description: fields.Description, SellingPoints: fields.SellingPoints,
		Category: fields.Category, SKU: fields.SKU, PriceMinor: fields.PriceMinor, Stock: fields.Stock, Status: fields.Status,
		Images: fields.Images, TiktokShopURL: fields.TiktokShopURL, ShopeeURL: fields.ShopeeURL, LazadaURL: fields.LazadaURL,
	}
	if err := f.repo.CreateProduct(ctx, p); err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	return f.GetProduct(ctx, p.ID.String(), userID)
}

func (f *MarketplaceFacade) UpdateProduct(ctx context.Context, id, userID string, in MarketplaceProductInput) (*dto_open_api.MarketplaceProductDTO, error) {
	p, uid, err := f.product(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	fields, err := f.productFields(ctx, userID, in, marketplaceImages(p.Images))
	if err != nil {
		return nil, err
	}
	if err := f.repo.UpdateProduct(ctx, p.ID, uid, fields); err != nil {
		return nil, libraryRepoErr("update product", err)
	}
	return f.GetProduct(ctx, id, userID)
}

func (f *MarketplaceFacade) DeleteProduct(ctx context.Context, id, userID string) error {
	pid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	return libraryRepoErr("delete product", f.repo.DeleteProduct(ctx, pid, uid))
}

// ── Contents ──────────────────────────────────────────────────────────────────

func marketplaceContentDTO(c models.MarketplaceContent) dto_open_api.MarketplaceContentDTO {
	d := dto_open_api.MarketplaceContentDTO{
		ID: c.ID.String(), ProductID: c.ProductID.String(), Kind: c.Kind, Title: c.Title, Brief: c.Brief,
		RefType: c.RefType, CreatedAt: libraryTime(c.CreatedAt),
	}
	if c.RefID != nil {
		id := c.RefID.String()
		d.RefID = &id
	}
	return d
}

func (f *MarketplaceFacade) ListContents(ctx context.Context, productID, userID string) ([]dto_open_api.MarketplaceContentDTO, error) {
	p, uid, err := f.product(ctx, productID, userID)
	if err != nil {
		return nil, err
	}
	rows, err := f.repo.ListContents(ctx, p.ID, uid)
	if err != nil {
		return nil, fmt.Errorf("list contents: %w", err)
	}
	out := make([]dto_open_api.MarketplaceContentDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, marketplaceContentDTO(c))
	}
	return out, nil
}

func (f *MarketplaceFacade) CreateContent(ctx context.Context, productID, userID string, in MarketplaceContentInput) (*dto_open_api.MarketplaceContentDTO, error) {
	p, uid, err := f.product(ctx, productID, userID)
	if err != nil {
		return nil, err
	}
	c := &models.MarketplaceContent{UserID: uid, ProductID: p.ID, Kind: in.Kind, Title: strings.TrimSpace(in.Title), Brief: strings.TrimSpace(in.Brief)}
	if (c.Kind != "promo_video" && c.Kind != "short_film") || c.Title == "" {
		return nil, ErrValidation
	}
	hasRef := in.RefID != nil && *in.RefID != ""
	if hasRef != (in.RefType != "") {
		return nil, ErrValidation
	}
	if hasRef {
		if in.RefType != "director_series" && in.RefType != "film_series" {
			return nil, ErrValidation
		}
		ref, err := uuid.Parse(*in.RefID)
		if err != nil {
			return nil, ErrValidation
		}
		c.RefType, c.RefID = in.RefType, &ref
	}
	existing, err := f.repo.ListContents(ctx, p.ID, uid)
	if err != nil {
		return nil, fmt.Errorf("list contents: %w", err)
	}
	if len(existing) >= maxMarketplaceContents {
		return nil, ErrMarketplaceLimit
	}
	if err := f.repo.CreateContent(ctx, c); err != nil {
		return nil, fmt.Errorf("create content: %w", err)
	}
	d := marketplaceContentDTO(*c)
	return &d, nil
}

func (f *MarketplaceFacade) DeleteContent(ctx context.Context, id, userID string) error {
	cid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	return libraryRepoErr("delete content", f.repo.DeleteContent(ctx, cid, uid))
}

// ── Publishes: a log of posts the user made on the platform themselves ────────

func marketplacePublishDTO(p models.MarketplacePublish) dto_open_api.MarketplacePublishDTO {
	d := dto_open_api.MarketplacePublishDTO{
		ID: p.ID.String(), ProductID: p.ProductID.String(), Platform: p.Platform, Caption: p.Caption, PostURL: p.PostURL,
		PostedOn: p.PostedOn.Format(marketplaceDateShape), CreatedAt: libraryTime(p.CreatedAt),
	}
	if p.ContentID != nil {
		id := p.ContentID.String()
		d.ContentID = &id
	}
	return d
}

func (f *MarketplaceFacade) ListPublishes(ctx context.Context, productID, userID string) ([]dto_open_api.MarketplacePublishDTO, error) {
	p, uid, err := f.product(ctx, productID, userID)
	if err != nil {
		return nil, err
	}
	rows, err := f.repo.ListPublishes(ctx, p.ID, uid)
	if err != nil {
		return nil, fmt.Errorf("list publishes: %w", err)
	}
	out := make([]dto_open_api.MarketplacePublishDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, marketplacePublishDTO(r))
	}
	return out, nil
}

func (f *MarketplaceFacade) CreatePublish(ctx context.Context, productID, userID string, in MarketplacePublishInput) (*dto_open_api.MarketplacePublishDTO, error) {
	p, uid, err := f.product(ctx, productID, userID)
	if err != nil {
		return nil, err
	}
	if !marketplacePlatform(in.Platform) {
		return nil, ErrValidation
	}
	day, err := f.day(in.PostedOn)
	if err != nil {
		return nil, err
	}
	link, err := marketplaceURL(in.PostURL)
	if err != nil {
		return nil, err
	}
	row := &models.MarketplacePublish{UserID: uid, ProductID: p.ID, Platform: in.Platform, Caption: strings.TrimSpace(in.Caption), PostURL: link, PostedOn: day}
	if in.ContentID != nil && *in.ContentID != "" {
		cid, err := uuid.Parse(*in.ContentID)
		if err != nil {
			return nil, ErrValidation
		}
		content, err := f.repo.FindContent(ctx, cid, uid)
		if errors.Is(err, repository_open_api.ErrNotFound) || (err == nil && content.ProductID != p.ID) {
			return nil, ErrValidation
		}
		if err != nil {
			return nil, fmt.Errorf("find content: %w", err)
		}
		row.ContentID = &cid
	}
	existing, err := f.repo.ListPublishes(ctx, p.ID, uid)
	if err != nil {
		return nil, fmt.Errorf("list publishes: %w", err)
	}
	if len(existing) >= maxMarketplacePublishes {
		return nil, ErrMarketplaceLimit
	}
	if err := f.repo.CreatePublish(ctx, row); err != nil {
		return nil, fmt.Errorf("create publish: %w", err)
	}
	d := marketplacePublishDTO(*row)
	return &d, nil
}

func (f *MarketplaceFacade) DeletePublish(ctx context.Context, id, userID string) error {
	pid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	return libraryRepoErr("delete publish", f.repo.DeletePublish(ctx, pid, uid))
}

// ── Sales ─────────────────────────────────────────────────────────────────────

func marketplaceSalesDays(days int) int {
	switch {
	case days <= 0:
		return marketplaceRecentDays
	case days < 7:
		return 7
	case days > 365:
		return 365
	}
	return days
}

func (f *MarketplaceFacade) GetSales(ctx context.Context, productID, userID string, days int) (*dto_open_api.MarketplaceSalesDTO, error) {
	p, uid, err := f.product(ctx, productID, userID)
	if err != nil {
		return nil, err
	}
	days = marketplaceSalesDays(days)
	to := f.today()
	from := to.AddDate(0, 0, -(days - 1))
	rows, err := f.repo.ListSales(ctx, p.ID, uid, from)
	if err != nil {
		return nil, fmt.Errorf("list sales: %w", err)
	}
	out := &dto_open_api.MarketplaceSalesDTO{
		Days: days, From: from.Format(marketplaceDateShape), To: to.Format(marketplaceDateShape),
		ByPlatform: []dto_open_api.MarketplaceSalesTotalDTO{}, Rows: make([]dto_open_api.MarketplaceSaleDTO, 0, len(rows)),
	}
	type sum struct{ units, orders, revenue int64 }
	per := map[string]*sum{}
	var total sum
	for _, r := range rows {
		out.Rows = append(out.Rows, dto_open_api.MarketplaceSaleDTO{
			ID: r.ID.String(), Platform: r.Platform, Date: r.SaleDate.Format(marketplaceDateShape),
			Units: r.Units, Orders: r.Orders, Revenue: marketplaceBaht(r.RevenueMinor),
		})
		s := per[r.Platform]
		if s == nil {
			s = &sum{}
			per[r.Platform] = s
		}
		for _, acc := range []*sum{s, &total} {
			acc.units += int64(r.Units)
			acc.orders += int64(r.Orders)
			acc.revenue += r.RevenueMinor
		}
	}
	out.Total = dto_open_api.MarketplaceSalesTotalDTO{Units: total.units, Orders: total.orders, Revenue: marketplaceBaht(total.revenue)}
	for _, platform := range marketplacePlatforms {
		if s := per[platform]; s != nil {
			out.ByPlatform = append(out.ByPlatform, dto_open_api.MarketplaceSalesTotalDTO{Platform: platform, Units: s.units, Orders: s.orders, Revenue: marketplaceBaht(s.revenue)})
		}
	}
	return out, nil
}

// UpsertSales replaces the figure for each (platform, day) given; a later duplicate in the batch wins.
func (f *MarketplaceFacade) UpsertSales(ctx context.Context, productID, userID string, in []MarketplaceSaleInput) error {
	p, uid, err := f.product(ctx, productID, userID)
	if err != nil {
		return err
	}
	if len(in) == 0 || len(in) > maxMarketplaceSaleRows {
		return ErrValidation
	}
	index := map[string]int{}
	rows := make([]models.MarketplaceSale, 0, len(in))
	for _, s := range in {
		if !marketplacePlatform(s.Platform) || s.Units < 0 || s.Orders < 0 {
			return ErrValidation
		}
		day, err := f.day(s.Date)
		if err != nil {
			return err
		}
		revenue, err := marketplaceMinor(s.Revenue)
		if err != nil {
			return err
		}
		row := models.MarketplaceSale{UserID: uid, ProductID: p.ID, Platform: s.Platform, SaleDate: day, Units: s.Units, Orders: s.Orders, RevenueMinor: revenue}
		key := s.Platform + "|" + s.Date
		if i, ok := index[key]; ok {
			rows[i] = row
			continue
		}
		index[key] = len(rows)
		rows = append(rows, row)
	}
	if err := f.repo.UpsertSales(ctx, rows); err != nil {
		return fmt.Errorf("upsert sales: %w", err)
	}
	return nil
}

func (f *MarketplaceFacade) DeleteSale(ctx context.Context, id, userID string) error {
	sid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	return libraryRepoErr("delete sale", f.repo.DeleteSale(ctx, sid, uid))
}
