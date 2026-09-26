package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Product struct {
	ID              int64
	Slug            string
	Name            string
	Description     string
	CategoryID      *int64 `db:"category_id"`
	MetaDescription string `db:"meta_description"`
	Tagline         string
	Active          bool
	Featured        bool
	Promo           bool
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

type Variant struct {
	ID           int64
	ProductID    int64 `db:"product_id"`
	Name         string
	SKU          *string `db:"sku"`
	Price        int
	RegularPrice *int `db:"regular_price"` // shown crossed out when greater than Price
	Stock        int
	Sort         int
	// SkipStockUpdate leaves an existing variant's stock column untouched on
	// UpdateProduct instead of overwriting it with Stock. Its zero value
	// (false) always writes Stock, matching every caller's existing
	// behaviour; only the admin product form sets it, and only when the
	// submitted stock equals the value the form was rendered with, i.e. the
	// admin did not touch that field (C2). It has no effect on insert.
	SkipStockUpdate bool `db:"-"`
}

type ProductFull struct {
	Product
	Category *Category
	Variants []Variant
	Images   []Image
}

// ProductCard is one tile in a grid: product plus cheapest price and first image.
type ProductCard struct {
	ID           int64
	Slug         string
	Name         string
	Tagline      string
	MinPrice     int     `db:"min_price"`
	VariantCount int     `db:"variant_count"`
	InStock      bool    `db:"in_stock"`
	VariantID    int64   `db:"variant_id"`    // cheapest variant: quick add-to-cart on single-variant products
	VariantName  string  `db:"variant_name"`  // cheapest variant's name, e.g. "100g"
	RegularPrice *int    `db:"regular_price"` // cheapest variant's regular price
	ImageFile    *string `db:"image_file"`
	ImageAlt     *string `db:"image_alt"`
	ImageWidth   *int    `db:"image_width"`
	ImageHeight  *int    `db:"image_height"`
}

type ListOpts struct {
	CategorySlug string
	CategoryID   int64 // 0 = any
	Query        string
	FeaturedOnly bool
	ExcludeID    int64
	Limit        int // 0 = 200
}

type AdminProductRow struct {
	ID           int64
	Slug         string
	Name         string
	Category     string
	VariantCount int `db:"variant_count"`
	Active       bool
	Featured     bool
	ImageFile    *string `db:"image_file"`
	ImageWidth   *int    `db:"image_width"` // the stored file's actual width, so imgURL never asks for a wider variant than exists
}

type SitemapEntry struct {
	Slug      string
	UpdatedAt time.Time `db:"updated_at"`
}

const productCols = `id, slug, name, description, category_id, meta_description, tagline, active, featured, promo, created_at, updated_at`
const variantCols = `id, product_id, name, sku, price, regular_price, stock, sort`

func (s *Store) ListProductCards(ctx context.Context, o ListOpts) ([]ProductCard, error) {
	if o.Limit == 0 {
		o.Limit = 200
	}
	rows, err := s.db.Query(ctx, `
		select p.id, p.slug, p.name, p.tagline,
		       coalesce(v.min_price, 0) as min_price,
		       coalesce(v.n, 0)::int as variant_count,
		       coalesce(v.in_stock, false) as in_stock,
		       coalesce(m.id, 0) as variant_id, coalesce(m.name, '') as variant_name, m.regular_price,
		       i.file as image_file, i.alt as image_alt, i.width as image_width, i.height as image_height
		from products p
		left join lateral (select min(price) min_price, count(*) n, bool_or(stock > 0) in_stock
		                   from variants where product_id = p.id) v on true
		left join lateral (select id, name, regular_price from variants
		                   where product_id = p.id order by price, sort, id limit 1) m on true
		left join lateral (select file, alt, width, height from product_images
		                   where product_id = p.id order by sort, id limit 1) i on true
		where p.active
		  and ($1 = '' or p.category_id = (select id from categories where slug = $1))
		  and ($2 = 0 or p.category_id = $2)
		  and ($3 = '' or p.name ilike '%' || $3 || '%')
		  and (not $4 or p.featured)
		  and p.id <> $5
		order by p.name
		limit $6`,
		o.CategorySlug, o.CategoryID, o.Query, o.FeaturedOnly, o.ExcludeID, o.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ProductCard])
}

func (s *Store) getProductWhere(ctx context.Context, where string, arg any, activeOnly bool) (ProductFull, error) {
	var f ProductFull
	rows, _ := s.db.Query(ctx, `select `+productCols+` from products where `+where+` and (not $2 or active)`, arg, activeOnly)
	p, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Product])
	if err != nil {
		return f, mapErr(err)
	}
	f.Product = p
	if p.CategoryID != nil {
		c, err := s.GetCategory(ctx, *p.CategoryID)
		if err == nil {
			f.Category = &c
		} else if !errors.Is(err, ErrNotFound) {
			return f, err
		}
	}
	vrows, err := s.db.Query(ctx, `select `+variantCols+` from variants where product_id = $1 order by sort, id`, p.ID)
	if err != nil {
		return f, err
	}
	if f.Variants, err = pgx.CollectRows(vrows, pgx.RowToStructByName[Variant]); err != nil {
		return f, err
	}
	f.Images, err = s.ListImages(ctx, p.ID)
	return f, err
}

func (s *Store) GetProductBySlug(ctx context.Context, slug string, activeOnly bool) (ProductFull, error) {
	return s.getProductWhere(ctx, "slug = $1", slug, activeOnly)
}

func (s *Store) GetProduct(ctx context.Context, id int64) (ProductFull, error) {
	return s.getProductWhere(ctx, "id = $1", id, false)
}

// GetPromo returns the active promo product (the home page banner), or
// ErrNotFound when there is none.
func (s *Store) GetPromo(ctx context.Context) (ProductFull, error) {
	return s.getProductWhere(ctx, "promo = $1", true, true)
}

func (s *Store) ListProductsAdmin(ctx context.Context) ([]AdminProductRow, error) {
	rows, err := s.db.Query(ctx, `
		select p.id, p.slug, p.name, coalesce(c.name, '') as category,
		       (select count(*) from variants where product_id = p.id)::int as variant_count,
		       p.active, p.featured,
		       i.file as image_file, i.width as image_width
		from products p left join categories c on c.id = p.category_id
		left join lateral (select file, width from product_images
		                   where product_id = p.id order by sort, id limit 1) i on true
		order by p.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[AdminProductRow])
}

func (s *Store) CreateProduct(ctx context.Context, p Product, vs []Variant) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if p.Promo {
		if err := clearPromo(ctx, tx, 0); err != nil {
			return 0, err
		}
	}
	var id int64
	err = tx.QueryRow(ctx, `insert into products (slug, name, description, category_id, meta_description, tagline, active, featured, promo)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9) returning id`,
		p.Slug, p.Name, p.Description, p.CategoryID, p.MetaDescription, p.Tagline, p.Active, p.Featured, p.Promo).Scan(&id)
	if err != nil {
		return 0, mapErr(err)
	}
	if err := syncVariants(ctx, tx, id, vs); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// UpdateProduct saves the product and makes its variants match vs exactly:
// rows with ID > 0 are updated, ID == 0 inserted, and existing rows not in vs deleted.
func (s *Store) UpdateProduct(ctx context.Context, p Product, vs []Variant) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Take the promo lock before locking our own row. The other order
	// deadlocks: tx A holds row A and waits for the lock, tx B holds the
	// lock and waits for row A while clearing it.
	if p.Promo {
		if err := clearPromo(ctx, tx, p.ID); err != nil {
			return err
		}
	}

	// Lock the row and read the slug it currently has, so the retired slug is
	// captured from the row we are about to overwrite rather than from a read
	// the caller did earlier. Two concurrent renames then serialize here: the
	// second one blocks until the first commits and so sees the first one's
	// new slug as ITS old slug, and both retired values end up in
	// product_slugs instead of one being silently lost (Task 21 fix round 1).
	var oldSlug string
	if err := tx.QueryRow(ctx, `select slug from products where id = $1 for update`, p.ID).Scan(&oldSlug); err != nil {
		return mapErr(err)
	}
	tag, err := tx.Exec(ctx, `update products set slug = $2, name = $3, description = $4, category_id = $5,
		meta_description = $6, tagline = $7, active = $8, featured = $9, promo = $10, updated_at = clock_timestamp() where id = $1`,
		p.ID, p.Slug, p.Name, p.Description, p.CategoryID, p.MetaDescription, p.Tagline, p.Active, p.Featured, p.Promo)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if oldSlug != p.Slug {
		// product_slugs.slug is the primary key: a slug retired by one
		// product and later retired again by another (after being reused)
		// must not error on the duplicate key — the newest owner wins.
		if _, err := tx.Exec(ctx, `insert into product_slugs (slug, product_id) values ($1, $2)
			on conflict (slug) do update set product_id = excluded.product_id`, oldSlug, p.ID); err != nil {
			return mapErr(err)
		}
	}
	if err := syncVariants(ctx, tx, p.ID, vs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func syncVariants(ctx context.Context, tx pgx.Tx, productID int64, vs []Variant) error {
	keep := []int64{0}
	for _, v := range vs {
		var sku *string
		if v.SKU != nil && *v.SKU != "" {
			sku = v.SKU
		}
		if v.ID > 0 {
			if v.SkipStockUpdate {
				// The admin form was submitted with the same stock value it
				// was rendered with, so the admin did not touch that field.
				// Leave the stock column alone: an order placed after the
				// form was opened but before it was saved must not have its
				// decrement overwritten back to the stale rendered value
				// (C2).
				if _, err := tx.Exec(ctx, `update variants set name = $3, sku = $4, price = $5, regular_price = $6, sort = $7 where id = $1 and product_id = $2`,
					v.ID, productID, v.Name, sku, v.Price, v.RegularPrice, v.Sort); err != nil {
					return mapErr(err)
				}
			} else {
				if _, err := tx.Exec(ctx, `update variants set name = $3, sku = $4, price = $5, regular_price = $6, stock = $7, sort = $8 where id = $1 and product_id = $2`,
					v.ID, productID, v.Name, sku, v.Price, v.RegularPrice, v.Stock, v.Sort); err != nil {
					return mapErr(err)
				}
			}
			keep = append(keep, v.ID)
		} else {
			var id int64
			if err := tx.QueryRow(ctx, `insert into variants (product_id, name, sku, price, regular_price, stock, sort) values ($1, $2, $3, $4, $5, $6, $7) returning id`,
				productID, v.Name, sku, v.Price, v.RegularPrice, v.Stock, v.Sort).Scan(&id); err != nil {
				return mapErr(err)
			}
			keep = append(keep, id)
		}
	}
	_, err := tx.Exec(ctx, `delete from variants where product_id = $1 and id <> all($2)`, productID, keep)
	return mapErr(err)
}

// clearPromo unsets promo on every product except keepID so the
// products_one_promo index never fires. The advisory lock serializes two
// concurrent promo saves; without it both could clear, both set, and the
// second commit would fail on the unique index.
func clearPromo(ctx context.Context, tx pgx.Tx, keepID int64) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('products_promo'))`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update products set promo = false where promo and id <> $1`, keepID)
	return err
}

// DeleteProduct removes the product and returns its image rows so the caller
// can delete files. Refuses with ErrInUse if any order references its variants.
func (s *Store) DeleteProduct(ctx context.Context, id int64) ([]Image, error) {
	var used bool
	if err := s.db.QueryRow(ctx, `select exists(select 1 from order_items oi join variants v on v.id = oi.variant_id where v.product_id = $1)`, id).Scan(&used); err != nil {
		return nil, err
	}
	if used {
		return nil, ErrInUse
	}
	imgs, err := s.ListImages(ctx, id)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `delete from products where id = $1`, id)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return imgs, nil
}

func (s *Store) ActiveProductsForSitemap(ctx context.Context) ([]SitemapEntry, error) {
	rows, err := s.db.Query(ctx, `select slug, updated_at from products where active order by slug`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[SitemapEntry])
}

// CartVariant is everything the cart needs to show one line.
type CartVariant struct {
	VariantID   int64  `db:"variant_id"`
	ProductID   int64  `db:"product_id"`
	ProductSlug string `db:"product_slug"`
	ProductName string `db:"product_name"`
	VariantName string `db:"variant_name"`
	Price       int
	Stock       int
	Active      bool
	ImageFile   *string `db:"image_file"`
	ImageAlt    *string `db:"image_alt"`
	ImageWidth  *int    `db:"image_width"`
	ImageHeight *int    `db:"image_height"`
}

func (s *Store) VariantsForCart(ctx context.Context, ids []int64) ([]CartVariant, error) {
	rows, err := s.db.Query(ctx, `
		select v.id as variant_id, p.id as product_id, p.slug as product_slug, p.name as product_name,
		       v.name as variant_name, v.price, v.stock, p.active,
		       i.file as image_file, i.alt as image_alt, i.width as image_width, i.height as image_height
		from variants v join products p on p.id = v.product_id
		left join lateral (select file, alt, width, height from product_images where product_id = p.id order by sort, id limit 1) i on true
		where v.id = any($1)`, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[CartVariant])
}
