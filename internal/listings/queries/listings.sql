-- name: UpsertListing :one
-- Inserts a listing or refreshes a known one. xmax is zero only on a fresh
-- insert, which tells the caller whether the listing is new.
INSERT INTO listings (
    source, external_id, title, url, category, district, address,
    rooms, area_m2, series, price, price_value, has_photo, image_url, posted_at
) VALUES (
    @source, @external_id, @title, @url, @category, @district, @address,
    @rooms, @area_m2, @series, @price, @price_value, @has_photo, @image_url, @posted_at
)
ON CONFLICT (source, external_id) DO UPDATE SET
    title        = EXCLUDED.title,
    url          = EXCLUDED.url,
    category     = EXCLUDED.category,
    district     = EXCLUDED.district,
    address      = EXCLUDED.address,
    rooms        = EXCLUDED.rooms,
    area_m2      = EXCLUDED.area_m2,
    series       = EXCLUDED.series,
    price        = EXCLUDED.price,
    price_value  = EXCLUDED.price_value,
    has_photo    = EXCLUDED.has_photo,
    image_url    = EXCLUDED.image_url,
    last_seen_at = now(),
    is_active    = TRUE
RETURNING (xmax = 0)::boolean AS is_new;
