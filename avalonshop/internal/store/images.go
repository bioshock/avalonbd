package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Image struct {
	ID        int64
	ProductID int64 `db:"product_id"`
	File      string
	Alt       string
	Width     int
	Height    int
	Sort      int
}

const imageCols = `id, product_id, file, alt, width, height, sort`

func (s *Store) ListImages(ctx context.Context, productID int64) ([]Image, error) {
	rows, err := s.db.Query(ctx, `select `+imageCols+` from product_images where product_id = $1 order by sort, id`, productID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Image])
}

// AddImage inserts the row and bumps the product's updated_at in one
// transaction, so a failure of either statement leaves neither committed: a
// non-nil error always means nothing was written, and the caller can safely
// clean up the files it already wrote to disk (task-17-review Minor 8).
func (s *Store) AddImage(ctx context.Context, img Image) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `insert into product_images (product_id, file, alt, width, height, sort)
		values ($1, $2, $3, $4, $5, (select coalesce(max(sort), 0) + 1 from product_images where product_id = $1)) returning id`,
		img.ProductID, img.File, img.Alt, img.Width, img.Height).Scan(&id); err != nil {
		return 0, mapErr(err)
	}
	if _, err := tx.Exec(ctx, `update products set updated_at = clock_timestamp() where id = $1`, img.ProductID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// UpdateImageAlt returns the image's product id (read back from the row
// itself, not trusted from the caller) so the handler can redirect to the
// right product without relying on a form field (task-17-review Minor 6).
func (s *Store) UpdateImageAlt(ctx context.Context, id int64, alt string) (int64, error) {
	var productID int64
	err := s.db.QueryRow(ctx, `update product_images set alt = $2 where id = $1 returning product_id`, id, alt).Scan(&productID)
	if err != nil {
		return 0, mapErr(err)
	}
	return productID, nil
}

// MoveImage swaps sort with the previous (up) or next (down) image of the same
// product. Moving past the end is a no-op. It returns the image's product id,
// read from the row itself, for the same reason as UpdateImageAlt.
func (s *Store) MoveImage(ctx context.Context, id int64, up bool) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, _ := tx.Query(ctx, `select `+imageCols+` from product_images where product_id = (select product_id from product_images where id = $1) order by sort, id for update`, id)
	imgs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Image])
	if err != nil {
		return 0, err
	}
	idx := -1
	for i, im := range imgs {
		if im.ID == id {
			idx = i
		}
	}
	if idx == -1 {
		return 0, ErrNotFound
	}
	productID := imgs[idx].ProductID
	j := idx + 1
	if up {
		j = idx - 1
	}
	if j < 0 || j >= len(imgs) {
		return productID, nil
	}
	imgs[idx], imgs[j] = imgs[j], imgs[idx]
	for i, im := range imgs { // renumber 1..n so ties from AddImage never matter
		if _, err := tx.Exec(ctx, `update product_images set sort = $2 where id = $1`, im.ID, i+1); err != nil {
			return 0, err
		}
	}
	return productID, tx.Commit(ctx)
}

// DeleteImage removes the row and bumps the product's updated_at in one
// transaction, so a failure of either statement leaves the row in place and
// the caller does not orphan files for a row that is still there
// (task-17-review Minor 8).
func (s *Store) DeleteImage(ctx context.Context, id int64) (Image, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Image{}, err
	}
	defer tx.Rollback(ctx)
	rows, _ := tx.Query(ctx, `delete from product_images where id = $1 returning `+imageCols, id)
	img, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Image])
	if err != nil {
		return img, mapErr(err)
	}
	if _, err := tx.Exec(ctx, `update products set updated_at = clock_timestamp() where id = $1`, img.ProductID); err != nil {
		return img, err
	}
	return img, tx.Commit(ctx)
}
