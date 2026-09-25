package store

import "context"

// RecordOldSlug records slug as a historical alias for productID, so that a
// later GET /products/{slug} on that no-longer-current value can 301 to the
// product's live URL instead of 404ing (Task 21).
//
// product_slugs.slug is the table's primary key: one retired slug names
// exactly one product. If some other product later claims and then retires
// the same slug value, this upserts rather than erroring on the duplicate
// key, so the newest owner wins the redirect.
//
// The WHERE clause guards against ever writing a product's own CURRENT slug
// into its history: if slug already equals products.slug for productID, the
// select side returns no rows and the insert is a silent no-op. This matters
// for a rename-and-rename-back sequence — without it, calling this with a
// stale "old slug" value that happens to already match the live slug again
// would still be harmless to serve (the live lookup always wins), but it is
// not a real historical alias and shouldn't be written as one.
func (s *Store) RecordOldSlug(ctx context.Context, productID int64, slug string) error {
	_, err := s.db.Exec(ctx, `
		insert into product_slugs (slug, product_id)
		select $1, $2
		where $1 <> (select slug from products where id = $2)
		on conflict (slug) do update set product_id = excluded.product_id`,
		slug, productID)
	return mapErr(err)
}

// ResolveSlugRedirect looks up slug among retired slugs and, if its owning
// product is active, returns that product's current slug. Callers must check
// the live products.slug first and only fall back to this on a miss there —
// the live product always wins, and a history row must never shadow it.
//
// The join filters on p.active so an old slug whose product is inactive or
// deleted returns ErrNotFound, exactly like that product's own current slug
// would. A permanent redirect to a page that then 404s is worse than 404ing
// directly: crawlers cache a 301 for a long time, and reactivating the
// product later would not undo the damage already cached
// (task-21-decisions #1).
func (s *Store) ResolveSlugRedirect(ctx context.Context, slug string) (string, error) {
	var current string
	err := s.db.QueryRow(ctx, `
		select p.slug from product_slugs h
		join products p on p.id = h.product_id
		where h.slug = $1 and p.active`, slug).Scan(&current)
	return current, mapErr(err)
}
