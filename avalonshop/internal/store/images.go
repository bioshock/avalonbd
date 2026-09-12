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

func (s *Store) AddImage(ctx context.Context, img Image) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into product_images (product_id, file, alt, width, height, sort)
		values ($1, $2, $3, $4, $5, (select coalesce(max(sort), 0) + 1 from product_images where product_id = $1)) returning id`,
		img.ProductID, img.File, img.Alt, img.Width, img.Height).Scan(&id)
	if err != nil {
		return 0, mapErr(err)
	}
	_, err = s.db.Exec(ctx, `update products set updated_at = clock_timestamp() where id = $1`, img.ProductID)
	return id, err
}

func (s *Store) UpdateImageAlt(ctx context.Context, id int64, alt string) error {
	tag, err := s.db.Exec(ctx, `update product_images set alt = $2 where id = $1`, id, alt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveImage swaps sort with the previous (up) or next (down) image of the same
// product. Moving past the end is a no-op.
func (s *Store) MoveImage(ctx context.Context, id int64, up bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, _ := tx.Query(ctx, `select `+imageCols+` from product_images where product_id = (select product_id from product_images where id = $1) order by sort, id for update`, id)
	imgs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Image])
	if err != nil {
		return err
	}
	idx := -1
	for i, im := range imgs {
		if im.ID == id {
			idx = i
		}
	}
	if idx == -1 {
		return ErrNotFound
	}
	j := idx + 1
	if up {
		j = idx - 1
	}
	if j < 0 || j >= len(imgs) {
		return nil
	}
	imgs[idx], imgs[j] = imgs[j], imgs[idx]
	for i, im := range imgs { // renumber 1..n so ties from AddImage never matter
		if _, err := tx.Exec(ctx, `update product_images set sort = $2 where id = $1`, im.ID, i+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteImage(ctx context.Context, id int64) (Image, error) {
	rows, _ := s.db.Query(ctx, `delete from product_images where id = $1 returning `+imageCols, id)
	img, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Image])
	if err != nil {
		return img, mapErr(err)
	}
	_, err = s.db.Exec(ctx, `update products set updated_at = clock_timestamp() where id = $1`, img.ProductID)
	return img, err
}
