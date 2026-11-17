# Marketplace API contract (manual phase)

All routes are under `/open/v1/marketplace` (user JWT). Responses use the standard envelope
`{ "data": ..., "requestId": ... }`; errors use `{ "code", "message", "requestId", "details?" }`.
Every resource is scoped to the authenticated user; another user's id answers `404 NOT_FOUND`.

This phase calls no TikTok Shop, Shopee or Lazada API. Channel fields are public storefront or
listing URLs typed by the user, posts are logged after the user publishes by hand, and sales are
entered or imported by the user. Money is sent as a number in baht and stored in satang.

| Method | Path | Body / query | Result |
|---|---|---|---|
| GET | `/shops` | `q`, `page`, `limit` | `{ items: Shop[], total, page, limit }` |
| POST | `/shops` | ShopBody | Shop (201) |
| GET | `/shops/:id` | | Shop, with `trends` |
| PUT | `/shops/:id` | ShopBody (full replace) | Shop |
| DELETE | `/shops/:id` | | 204; also removes the shop's products |
| POST | `/shops/:id/trends` | | Trends (AI, rate limited) |
| GET | `/shops/:id/products` | `q`, `status`, `page`, `limit` | `{ items: Product[], ... }` with 30-day `recentUnits`, `recentRevenue`, `contentCount`; only the cover image is signed |
| POST | `/shops/:id/products` | ProductBody | Product (201) |
| GET | `/products/:id` | | Product, with all images, `shopName`, `suggestion` |
| PUT | `/products/:id` | ProductBody (full replace) | Product |
| DELETE | `/products/:id` | | 204 |
| POST | `/products/:id/suggestions` | | Suggestion (AI, rate limited) |
| GET / POST | `/products/:id/contents` | ContentBody | `{ items: Content[] }` / Content (201) |
| DELETE | `/contents/:id` | | 204 |
| GET / POST | `/products/:id/publishes` | PublishBody | `{ items: Publish[] }` / Publish (201) |
| DELETE | `/publishes/:id` | | 204 |
| GET | `/products/:id/sales` | `days` (7-365, default 30) | `{ days, from, to, total, byPlatform[], rows[] }` |
| POST | `/products/:id/sales` | `{ rows: SaleRow[] }` (1-400) | 204; upserts by (platform, date) |
| DELETE | `/sales/:id` | | 204 |

Field shapes are the DTOs in `apps/dto/dto_open_api/marketplace_dto.go` and the requests in
`apps/requests/request_open_api/marketplace_requests.go`; the web mirror is `src/lib/marketplace-api.ts`.

Notes:

- Images: `logoUploadId` and `imageUploadIds` are ids from `POST /open/v1/uploads`. The storage key is
  copied onto the shop/product, so pictures outlive the 24-hour upload row. An id already attached is
  kept without a new lookup. Image URLs in responses are signed for one hour.
- Links (`website`, `*Url`, `postUrl`) must be http(s); a value without a scheme gets `https://`.
- Dates (`postedOn`, sale `date`) are `YYYY-MM-DD` calendar days in Thai time, not later than tomorrow.
- Content `refType` is `director_series` or `film_series` with `refId`, or both omitted.
- AI routes use the Director story planner's text model, cost no points, and are limited to
  6 requests per user per minute. Codes: `AI_NOT_CONFIGURED` (503), `AI_FAILED` (502), `RATE_LIMITED` (429).
  Trends come from the model's general knowledge, not live marketplace data.
- Limits: 50 shops per user, 500 products per shop, 6 images per product, 100 contents and
  200 publishes per product (`409 MARKETPLACE_LIMIT_REACHED`).
