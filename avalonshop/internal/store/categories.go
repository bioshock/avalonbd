package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Category struct {
	ID   int64
	Slug string
	Name string
	Sort int
}

const categoryCols = `id, slug, name, sort`

func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.Query(ctx, `select `+categoryCols+` from categories order by sort, name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Category])
}

func (s *Store) GetCategoryBySlug(ctx context.Context, slug string) (Category, error) {
	rows, _ := s.db.Query(ctx, `select `+categoryCols+` from categories where slug = $1`, slug)
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Category])
	return c, mapErr(err)
}

func (s *Store) GetCategory(ctx context.Context, id int64) (Category, error) {
	rows, _ := s.db.Query(ctx, `select `+categoryCols+` from categories where id = $1`, id)
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Category])
	return c, mapErr(err)
}

func (s *Store) CreateCategory(ctx context.Context, c Category) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into categories (slug, name, sort) values ($1, $2, $3) returning id`,
		c.Slug, c.Name, c.Sort).Scan(&id)
	return id, mapErr(err)
}

func (s *Store) UpdateCategory(ctx context.Context, c Category) error {
	tag, err := s.db.Exec(ctx, `update categories set slug = $2, name = $3, sort = $4 where id = $1`,
		c.ID, c.Slug, c.Name, c.Sort)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `delete from categories where id = $1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
