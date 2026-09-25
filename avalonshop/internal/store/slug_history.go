// Package store's product_slugs support. Writing a retired slug into history
// happens inside Store.UpdateProduct itself (internal/store/products.go),
// under the same row lock and transaction as the rename, so two concurrent
// renames of one product serialize on that lock instead of racing to record
// the same stale "old slug" and losing one of them (Task 21 fix round 1).
// This file only keeps the read side.
package store

import "context"

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
