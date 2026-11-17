package facade_open_api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

// fakeMarketplaceRepo keeps rows in memory and applies the same owner scoping as the SQL repository.
type fakeMarketplaceRepo struct {
	shops     map[uuid.UUID]*models.MarketplaceShop
	products  map[uuid.UUID]*models.MarketplaceProduct
	contents  map[uuid.UUID]*models.MarketplaceContent
	publishes map[uuid.UUID]*models.MarketplacePublish
	sales     map[string]*models.MarketplaceSale
}

func newFakeMarketplaceRepo() *fakeMarketplaceRepo {
	return &fakeMarketplaceRepo{
		shops: map[uuid.UUID]*models.MarketplaceShop{}, products: map[uuid.UUID]*models.MarketplaceProduct{},
		contents: map[uuid.UUID]*models.MarketplaceContent{}, publishes: map[uuid.UUID]*models.MarketplacePublish{},
		sales: map[string]*models.MarketplaceSale{},
	}
}

func (r *fakeMarketplaceRepo) ListShops(_ context.Context, uid uuid.UUID, _ string, _, _ int) ([]repository_open_api.MarketplaceShopRow, int64, error) {
	out := []repository_open_api.MarketplaceShopRow{}
	for _, s := range r.shops {
		if s.UserID == uid {
			out = append(out, repository_open_api.MarketplaceShopRow{MarketplaceShop: *s})
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeMarketplaceRepo) FindShop(_ context.Context, id, uid uuid.UUID) (*repository_open_api.MarketplaceShopRow, error) {
	s, ok := r.shops[id]
	if !ok || s.UserID != uid {
		return nil, repository_open_api.ErrNotFound
	}
	return &repository_open_api.MarketplaceShopRow{MarketplaceShop: *s}, nil
}

func (r *fakeMarketplaceRepo) CountShops(_ context.Context, uid uuid.UUID) (int64, error) {
	var n int64
	for _, s := range r.shops {
		if s.UserID == uid {
			n++
		}
	}
	return n, nil
}

func (r *fakeMarketplaceRepo) CreateShop(_ context.Context, s *models.MarketplaceShop) error {
	s.ID = uuid.New()
	r.shops[s.ID] = s
	return nil
}

func (r *fakeMarketplaceRepo) UpdateShop(_ context.Context, id, uid uuid.UUID, f repository_open_api.MarketplaceShopFields) error {
	s, ok := r.shops[id]
	if !ok || s.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	s.Name, s.Website, s.LogoUploadID, s.LogoKey = f.Name, f.Website, f.LogoUploadID, f.LogoKey
	return nil
}

func (r *fakeMarketplaceRepo) DeleteShop(_ context.Context, id, uid uuid.UUID) error {
	if s, ok := r.shops[id]; !ok || s.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	delete(r.shops, id)
	return nil
}

func (r *fakeMarketplaceRepo) SetShopTrends(_ context.Context, id, uid uuid.UUID, data []byte, at time.Time) error {
	s, ok := r.shops[id]
	if !ok || s.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	s.AITrends, s.AITrendsAt = data, &at
	return nil
}

func (r *fakeMarketplaceRepo) ListProducts(_ context.Context, shopID, uid uuid.UUID, _ repository_open_api.MarketplaceProductFilter) ([]repository_open_api.MarketplaceProductRow, int64, error) {
	out := []repository_open_api.MarketplaceProductRow{}
	for _, p := range r.products {
		if p.ShopID == shopID && p.UserID == uid {
			out = append(out, repository_open_api.MarketplaceProductRow{MarketplaceProduct: *p})
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeMarketplaceRepo) FindProduct(_ context.Context, id, uid uuid.UUID) (*models.MarketplaceProduct, error) {
	p, ok := r.products[id]
	if !ok || p.UserID != uid {
		return nil, repository_open_api.ErrNotFound
	}
	c := *p
	return &c, nil
}

func (r *fakeMarketplaceRepo) CountProducts(_ context.Context, shopID, uid uuid.UUID) (int64, error) {
	rows, n, _ := r.ListProducts(context.Background(), shopID, uid, repository_open_api.MarketplaceProductFilter{})
	_ = rows
	return n, nil
}

func (r *fakeMarketplaceRepo) CreateProduct(_ context.Context, p *models.MarketplaceProduct) error {
	p.ID = uuid.New()
	r.products[p.ID] = p
	return nil
}

func (r *fakeMarketplaceRepo) UpdateProduct(_ context.Context, id, uid uuid.UUID, f repository_open_api.MarketplaceProductFields) error {
	p, ok := r.products[id]
	if !ok || p.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	p.Name, p.PriceMinor, p.Images = f.Name, f.PriceMinor, f.Images
	return nil
}

func (r *fakeMarketplaceRepo) DeleteProduct(_ context.Context, id, uid uuid.UUID) error {
	if p, ok := r.products[id]; !ok || p.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	delete(r.products, id)
	return nil
}

func (r *fakeMarketplaceRepo) SetProductSuggestion(_ context.Context, id, uid uuid.UUID, data []byte, at time.Time) error {
	p, ok := r.products[id]
	if !ok || p.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	p.AISuggestion, p.AISuggestedAt = data, &at
	return nil
}

func (r *fakeMarketplaceRepo) ListContents(_ context.Context, productID, uid uuid.UUID) ([]models.MarketplaceContent, error) {
	out := []models.MarketplaceContent{}
	for _, c := range r.contents {
		if c.ProductID == productID && c.UserID == uid {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (r *fakeMarketplaceRepo) FindContent(_ context.Context, id, uid uuid.UUID) (*models.MarketplaceContent, error) {
	c, ok := r.contents[id]
	if !ok || c.UserID != uid {
		return nil, repository_open_api.ErrNotFound
	}
	return c, nil
}

func (r *fakeMarketplaceRepo) CreateContent(_ context.Context, c *models.MarketplaceContent) error {
	c.ID = uuid.New()
	r.contents[c.ID] = c
	return nil
}

func (r *fakeMarketplaceRepo) DeleteContent(_ context.Context, id, uid uuid.UUID) error {
	if c, ok := r.contents[id]; !ok || c.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	delete(r.contents, id)
	return nil
}

func (r *fakeMarketplaceRepo) ListPublishes(_ context.Context, productID, uid uuid.UUID) ([]models.MarketplacePublish, error) {
	out := []models.MarketplacePublish{}
	for _, p := range r.publishes {
		if p.ProductID == productID && p.UserID == uid {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (r *fakeMarketplaceRepo) CreatePublish(_ context.Context, p *models.MarketplacePublish) error {
	p.ID = uuid.New()
	r.publishes[p.ID] = p
	return nil
}

func (r *fakeMarketplaceRepo) DeletePublish(_ context.Context, id, uid uuid.UUID) error {
	if p, ok := r.publishes[id]; !ok || p.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	delete(r.publishes, id)
	return nil
}

func (r *fakeMarketplaceRepo) UpsertSales(_ context.Context, rows []models.MarketplaceSale) error {
	for _, s := range rows {
		s.ID = uuid.New()
		r.sales[s.ProductID.String()+s.Platform+s.SaleDate.Format("2006-01-02")] = &s
	}
	return nil
}

func (r *fakeMarketplaceRepo) ListSales(_ context.Context, productID, uid uuid.UUID, since time.Time) ([]models.MarketplaceSale, error) {
	out := []models.MarketplaceSale{}
	for _, s := range r.sales {
		if s.ProductID == productID && s.UserID == uid && !s.SaleDate.Before(since) {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (r *fakeMarketplaceRepo) DeleteSale(_ context.Context, id, uid uuid.UUID) error {
	for k, s := range r.sales {
		if s.ID == id && s.UserID == uid {
			delete(r.sales, k)
			return nil
		}
	}
	return repository_open_api.ErrNotFound
}

type fakeMarketplaceUploads struct {
	rows  map[string]*models.Upload
	calls int
}

func (u *fakeMarketplaceUploads) FindByID(_ context.Context, id, userID string) (*models.Upload, error) {
	u.calls++
	up, ok := u.rows[id]
	if !ok || up.UserID.String() != userID {
		return nil, repository_open_api.ErrNotFound
	}
	return up, nil
}

type fakeMarketplaceStorage struct{}

func (fakeMarketplaceStorage) SignedURL(_ context.Context, key string, _ time.Duration) (string, time.Time, error) {
	return "https://signed.test/" + key, time.Time{}, nil
}

type fakeMarketplaceAnalyzer struct {
	reply  string
	err    error
	prompt string
}

func (a *fakeMarketplaceAnalyzer) Analyze(_ context.Context, _, prompt string) (string, error) {
	a.prompt = prompt
	return a.reply, a.err
}

type marketplaceFixture struct {
	facade  *MarketplaceFacade
	repo    *fakeMarketplaceRepo
	uploads *fakeMarketplaceUploads
	ai      *fakeMarketplaceAnalyzer
	user    string
}

func newMarketplaceFixture() *marketplaceFixture {
	fx := &marketplaceFixture{
		repo: newFakeMarketplaceRepo(), uploads: &fakeMarketplaceUploads{rows: map[string]*models.Upload{}},
		ai: &fakeMarketplaceAnalyzer{}, user: uuid.New().String(),
	}
	fx.facade = NewMarketplaceFacade(fx.repo, fx.uploads, fakeMarketplaceStorage{}, MarketplaceOptions{Analyzer: fx.ai, AnalysisModel: "test-model"})
	fx.facade.now = func() time.Time { return time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC) }
	return fx
}

func (fx *marketplaceFixture) upload(t *testing.T, mime string) string {
	t.Helper()
	id := uuid.New()
	fx.uploads.rows[id.String()] = &models.Upload{Base: models.Base{ID: id}, UserID: uuid.MustParse(fx.user), StorageKey: "uploads/" + id.String(), MimeType: mime}
	return id.String()
}

func (fx *marketplaceFixture) shopAndProduct(t *testing.T) (shopID, productID string) {
	t.Helper()
	ctx := context.Background()
	shop, err := fx.facade.CreateShop(ctx, fx.user, MarketplaceShopInput{Name: " ร้านทดสอบ "})
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}
	p, err := fx.facade.CreateProduct(ctx, shop.ID, fx.user, MarketplaceProductInput{Name: "เซรั่ม", Price: 199.99})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	return shop.ID, p.ID
}

func TestMarketplaceShopLinksAreHTTPOnly(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	shop, err := fx.facade.CreateShop(ctx, fx.user, MarketplaceShopInput{Name: " ร้าน ", Website: "example.com/shop", ShopeeURL: "http://shopee.co.th/x"})
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}
	if shop.Name != "ร้าน" || shop.Website != "https://example.com/shop" || shop.ShopeeURL != "http://shopee.co.th/x" || shop.Status != "active" {
		t.Fatalf("unexpected shop: %+v", shop)
	}
	for _, bad := range []string{"javascript://alert(1)", "data://text/html,x", "ftp://host/file"} {
		if _, err := fx.facade.CreateShop(ctx, fx.user, MarketplaceShopInput{Name: "x", TiktokShopURL: bad}); !errors.Is(err, ErrValidation) {
			t.Fatalf("link %q: want ErrValidation, got %v", bad, err)
		}
	}
	if _, err := fx.facade.CreateShop(ctx, fx.user, MarketplaceShopInput{Name: "   "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name: want ErrValidation, got %v", err)
	}
}

func TestMarketplaceShopsAreOwnerScoped(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	shopID, productID := fx.shopAndProduct(t)
	other := uuid.New().String()
	if _, err := fx.facade.GetShop(ctx, shopID, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's shop: want ErrNotFound, got %v", err)
	}
	if _, err := fx.facade.GetProduct(ctx, productID, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's product: want ErrNotFound, got %v", err)
	}
	if _, err := fx.facade.CreateProduct(ctx, shopID, other, MarketplaceProductInput{Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("product in other user's shop: want ErrNotFound, got %v", err)
	}
	if err := fx.facade.UpsertSales(ctx, productID, other, []MarketplaceSaleInput{{Platform: "shopee", Date: "2026-11-12", Units: 1}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sales on other user's product: want ErrNotFound, got %v", err)
	}
	if _, err := fx.facade.GetShop(ctx, "not-a-uuid", fx.user); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed id: want ErrNotFound, got %v", err)
	}
}

func TestMarketplaceProductImages(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	shopID, _ := fx.shopAndProduct(t)
	first, second := fx.upload(t, "image/png"), fx.upload(t, "image/jpeg")
	p, err := fx.facade.CreateProduct(ctx, shopID, fx.user, MarketplaceProductInput{Name: "ครีม", Price: 10.005, ImageUploadIDs: []string{first, second}})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if len(p.Images) != 2 || p.Images[0].ID != first || p.Images[0].URL != "https://signed.test/uploads/"+first {
		t.Fatalf("unexpected images: %+v", p.Images)
	}

	// Uploads expire, so an image already on the product must be kept without looking the upload up again.
	delete(fx.uploads.rows, first)
	calls := fx.uploads.calls
	updated, err := fx.facade.UpdateProduct(ctx, p.ID, fx.user, MarketplaceProductInput{Name: "ครีม", ImageUploadIDs: []string{first}})
	if err != nil {
		t.Fatalf("update product: %v", err)
	}
	if len(updated.Images) != 1 || updated.Images[0].ID != first || fx.uploads.calls != calls {
		t.Fatalf("kept image was re-resolved or lost: %+v calls=%d", updated.Images, fx.uploads.calls-calls)
	}

	audio := fx.upload(t, "audio/mpeg")
	if _, err := fx.facade.UpdateProduct(ctx, p.ID, fx.user, MarketplaceProductInput{Name: "ครีม", ImageUploadIDs: []string{audio}}); !errors.Is(err, ErrInvalidFileType) {
		t.Fatalf("audio as image: want ErrInvalidFileType, got %v", err)
	}
	if _, err := fx.facade.UpdateProduct(ctx, p.ID, fx.user, MarketplaceProductInput{Name: "ครีม", ImageUploadIDs: []string{uuid.New().String()}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown upload: want ErrValidation, got %v", err)
	}
}

func TestMarketplaceMinorRounding(t *testing.T) {
	for amount, want := range map[float64]int64{0: 0, 199.99: 19999, 0.1 + 0.2: 30, 2500.5: 250050} {
		got, err := marketplaceMinor(amount)
		if err != nil || got != want {
			t.Fatalf("marketplaceMinor(%v) = %d, %v; want %d", amount, got, err, want)
		}
	}
	if _, err := marketplaceMinor(-1); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative amount: want ErrValidation, got %v", err)
	}
}

func TestMarketplaceSales(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	_, productID := fx.shopAndProduct(t)
	err := fx.facade.UpsertSales(ctx, productID, fx.user, []MarketplaceSaleInput{
		{Platform: "shopee", Date: "2026-11-12", Units: 1, Orders: 1, Revenue: 100},
		{Platform: "shopee", Date: "2026-11-12", Units: 3, Orders: 2, Revenue: 300.50},
		{Platform: "tiktok_shop", Date: "2026-11-13", Units: 5, Orders: 5, Revenue: 999.5},
		{Platform: "lazada", Date: "2026-09-01", Units: 9, Orders: 9, Revenue: 9},
	})
	if err != nil {
		t.Fatalf("upsert sales: %v", err)
	}
	sales, err := fx.facade.GetSales(ctx, productID, fx.user, 30)
	if err != nil {
		t.Fatalf("get sales: %v", err)
	}
	// The fixture clock is 10:00 on 13 Nov in Thailand; the September row is outside the 30-day window.
	if sales.To != "2026-11-13" || sales.From != "2026-10-15" || len(sales.Rows) != 2 {
		t.Fatalf("unexpected window: %+v", sales)
	}
	if sales.Total.Units != 8 || sales.Total.Orders != 7 || sales.Total.Revenue != 1300 {
		t.Fatalf("unexpected total: %+v", sales.Total)
	}
	if len(sales.ByPlatform) != 2 || sales.ByPlatform[0].Platform != "tiktok_shop" || sales.ByPlatform[1].Units != 3 {
		t.Fatalf("unexpected platform totals: %+v", sales.ByPlatform)
	}
	for _, bad := range []MarketplaceSaleInput{
		{Platform: "shopee", Date: "2026-11-20"},
		{Platform: "shopee", Date: "13/11/2026"},
		{Platform: "amazon", Date: "2026-11-12"},
		{Platform: "shopee", Date: "2026-11-12", Units: -1},
	} {
		if err := fx.facade.UpsertSales(ctx, productID, fx.user, []MarketplaceSaleInput{bad}); !errors.Is(err, ErrValidation) {
			t.Fatalf("row %+v: want ErrValidation, got %v", bad, err)
		}
	}
}

func TestMarketplacePublishMustReferenceOwnProductContent(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	shopID, productID := fx.shopAndProduct(t)
	other, err := fx.facade.CreateProduct(ctx, shopID, fx.user, MarketplaceProductInput{Name: "อีกชิ้น"})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	series := uuid.New().String()
	content, err := fx.facade.CreateContent(ctx, productID, fx.user, MarketplaceContentInput{Kind: "short_film", Title: "หนังสั้น", RefType: "film_series", RefID: &series})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}
	if _, err := fx.facade.CreateContent(ctx, productID, fx.user, MarketplaceContentInput{Kind: "promo_video", Title: "x", RefType: "film_series"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("ref type without id: want ErrValidation, got %v", err)
	}
	in := MarketplacePublishInput{ContentID: &content.ID, Platform: "tiktok_shop", PostURL: "tiktok.com/@shop/video/1", PostedOn: "2026-11-13"}
	post, err := fx.facade.CreatePublish(ctx, productID, fx.user, in)
	if err != nil {
		t.Fatalf("create publish: %v", err)
	}
	if post.PostURL != "https://tiktok.com/@shop/video/1" || post.ContentID == nil || *post.ContentID != content.ID {
		t.Fatalf("unexpected publish: %+v", post)
	}
	if _, err := fx.facade.CreatePublish(ctx, other.ID, fx.user, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("content of another product: want ErrValidation, got %v", err)
	}
}

const marketplaceSuggestionReply = "```json\n" + `{"similarProducts":["เซรั่มวิตามินซี","  "],
"angles":[{"format":"รีวิวใช้จริง","fit":"promo_video","title":"7 วันผิวเปลี่ยน","why":"เห็นผลชัด","hook":"หยุดก่อน!","outline":["เปิดปัญหา","โชว์สินค้า"]},
{"format":"เล่าเรื่อง","fit":"movie","title":"เช้าวันสำคัญ","why":"สร้างอารมณ์ร่วม","hook":"เธอเกือบไม่ไป","outline":[]},
{"format":"ไม่มีชื่อ","fit":"promo_video","title":""}],
"captions":[{"platform":"tiktok_shop","caption":"ลองเลย"},{"platform":"tiktok_shop","caption":"ซ้ำ"},{"platform":"amazon","caption":"x"}],
"hashtags":["เซรั่ม","#ผิว ใส","#"]}` + "\n```"

func TestMarketplaceSuggestion(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	_, productID := fx.shopAndProduct(t)

	fx.ai.reply = marketplaceSuggestionReply
	got, err := fx.facade.SuggestForProduct(ctx, productID, fx.user)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(got.SimilarProducts) != 1 || len(got.Angles) != 2 || got.Angles[1].Fit != "promo_video" || got.Angles[1].Outline == nil {
		t.Fatalf("unexpected angles: %+v", got)
	}
	if len(got.Captions) != 1 || got.Captions[0].Caption != "ลองเลย" {
		t.Fatalf("unexpected captions: %+v", got.Captions)
	}
	if strings.Join(got.Hashtags, " ") != "#เซรั่ม #ผิวใส" || got.GeneratedAt == "" {
		t.Fatalf("unexpected hashtags or timestamp: %+v", got)
	}
	if !strings.Contains(fx.ai.prompt, "เซรั่ม") || !strings.Contains(fx.ai.prompt, "199.99") {
		t.Fatalf("prompt is missing product data: %s", fx.ai.prompt)
	}
	stored, err := fx.facade.GetProduct(ctx, productID, fx.user)
	if err != nil || stored.Suggestion == nil || len(stored.Suggestion.Angles) != 2 {
		t.Fatalf("suggestion was not stored: %+v, %v", stored, err)
	}

	for _, reply := range []string{"ขออภัย ตอบไม่ได้", `{"angles":[]}`, `{"angles":`} {
		fx.ai.reply = reply
		if _, err := fx.facade.SuggestForProduct(ctx, productID, fx.user); !errors.Is(err, ErrMarketplaceAIFailed) {
			t.Fatalf("reply %q: want ErrMarketplaceAIFailed, got %v", reply, err)
		}
	}
	fx.ai.err = errors.New("provider down")
	if _, err := fx.facade.SuggestForProduct(ctx, productID, fx.user); !errors.Is(err, ErrMarketplaceAIFailed) {
		t.Fatalf("provider error: want ErrMarketplaceAIFailed, got %v", err)
	}
	fx.facade.options = MarketplaceOptions{}
	if _, err := fx.facade.SuggestForProduct(ctx, productID, fx.user); !errors.Is(err, ErrMarketplaceAIUnavailable) {
		t.Fatalf("no analyzer: want ErrMarketplaceAIUnavailable, got %v", err)
	}
}

func TestMarketplaceTrends(t *testing.T) {
	fx := newMarketplaceFixture()
	ctx := context.Background()
	shopID, _ := fx.shopAndProduct(t)
	fx.ai.reply = `{"items":[{"name":"ครีมกันแดด","category":"ความงาม","why":"อากาศร้อน","contentIdea":"เทียบก่อนหลัง"},{"name":""}]}`
	got, err := fx.facade.SuggestTrends(ctx, shopID, fx.user)
	if err != nil {
		t.Fatalf("trends: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "ครีมกันแดด" {
		t.Fatalf("unexpected trends: %+v", got)
	}
	if !strings.Contains(fx.ai.prompt, "พฤศจิกายน พ.ศ. 2569") {
		t.Fatalf("prompt is missing the current Thai month: %s", fx.ai.prompt)
	}
	shop, err := fx.facade.GetShop(ctx, shopID, fx.user)
	if err != nil || shop.Trends == nil || len(shop.Trends.Items) != 1 {
		t.Fatalf("trends were not stored: %+v, %v", shop, err)
	}
}
