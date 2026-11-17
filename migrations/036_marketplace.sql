BEGIN;

-- Marketplace menu, manual phase: shops, their products, promo content links, a log of posts
-- made by hand on TikTok Shop / Shopee / Lazada, and sales figures entered or imported by the user.
-- No platform credentials are stored; channel columns hold public storefront/listing URLs only.

CREATE TABLE marketplace_shops (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
  description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
  category text NOT NULL DEFAULT '' CHECK (char_length(category) <= 60),
  logo_upload_id uuid,
  logo_key text,
  phone text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 30),
  email text NOT NULL DEFAULT '' CHECK (char_length(email) <= 120),
  line_id text NOT NULL DEFAULT '' CHECK (char_length(line_id) <= 60),
  website text NOT NULL DEFAULT '' CHECK (char_length(website) <= 500),
  address text NOT NULL DEFAULT '' CHECK (char_length(address) <= 500),
  brand_tone text NOT NULL DEFAULT '' CHECK (char_length(brand_tone) <= 300),
  target_audience text NOT NULL DEFAULT '' CHECK (char_length(target_audience) <= 300),
  tiktok_shop_url text NOT NULL DEFAULT '' CHECK (char_length(tiktok_shop_url) <= 500),
  shopee_url text NOT NULL DEFAULT '' CHECK (char_length(shopee_url) <= 500),
  lazada_url text NOT NULL DEFAULT '' CHECK (char_length(lazada_url) <= 500),
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  ai_trends jsonb,
  ai_trends_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX marketplace_shops_owner ON marketplace_shops(user_id, created_at DESC) WHERE deleted_at IS NULL;

-- price_minor is satang (1/100 baht) so totals never accumulate floating-point error.
-- images is an ordered array of {"id": <upload id>, "key": <storage key>}.
CREATE TABLE marketplace_products (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  shop_id uuid NOT NULL REFERENCES marketplace_shops(id),
  name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
  description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
  selling_points text NOT NULL DEFAULT '' CHECK (char_length(selling_points) <= 1000),
  category text NOT NULL DEFAULT '' CHECK (char_length(category) <= 60),
  sku text NOT NULL DEFAULT '' CHECK (char_length(sku) <= 60),
  price_minor bigint NOT NULL DEFAULT 0 CHECK (price_minor >= 0),
  stock integer NOT NULL DEFAULT 0 CHECK (stock >= 0),
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','draft','archived')),
  images jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(images) = 'array' AND jsonb_array_length(images) <= 6),
  tiktok_shop_url text NOT NULL DEFAULT '' CHECK (char_length(tiktok_shop_url) <= 500),
  shopee_url text NOT NULL DEFAULT '' CHECK (char_length(shopee_url) <= 500),
  lazada_url text NOT NULL DEFAULT '' CHECK (char_length(lazada_url) <= 500),
  ai_suggestion jsonb,
  ai_suggested_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX marketplace_products_shop ON marketplace_products(shop_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX marketplace_products_owner ON marketplace_products(user_id) WHERE deleted_at IS NULL;

-- A promo video or promo short film made for a product. ref_* optionally points at the
-- Director series (ref_type 'director_series') or film series ('film_series') it was made in.
CREATE TABLE marketplace_contents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  product_id uuid NOT NULL REFERENCES marketplace_products(id),
  kind text NOT NULL CHECK (kind IN ('promo_video','short_film')),
  title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
  brief text NOT NULL DEFAULT '' CHECK (char_length(brief) <= 4000),
  ref_type text NOT NULL DEFAULT '' CHECK (ref_type IN ('','director_series','film_series')),
  ref_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CHECK ((ref_type = '') = (ref_id IS NULL))
);
CREATE INDEX marketplace_contents_product ON marketplace_contents(product_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE marketplace_publishes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  product_id uuid NOT NULL REFERENCES marketplace_products(id),
  content_id uuid REFERENCES marketplace_contents(id) ON DELETE SET NULL,
  platform text NOT NULL CHECK (platform IN ('tiktok_shop','shopee','lazada')),
  caption text NOT NULL DEFAULT '' CHECK (char_length(caption) <= 2200),
  post_url text NOT NULL DEFAULT '' CHECK (char_length(post_url) <= 500),
  posted_on date NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX marketplace_publishes_product ON marketplace_publishes(product_id, posted_on DESC) WHERE deleted_at IS NULL;

-- One row per product, platform and day; re-entering a day replaces its figures. Rows are
-- hard-deleted so the unique key stays usable.
CREATE TABLE marketplace_sales (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  product_id uuid NOT NULL REFERENCES marketplace_products(id),
  platform text NOT NULL CHECK (platform IN ('tiktok_shop','shopee','lazada')),
  sale_date date NOT NULL,
  units integer NOT NULL DEFAULT 0 CHECK (units >= 0),
  orders integer NOT NULL DEFAULT 0 CHECK (orders >= 0),
  revenue_minor bigint NOT NULL DEFAULT 0 CHECK (revenue_minor >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (product_id, platform, sale_date)
);

COMMIT;
