package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Zone struct {
	ID     int64
	Name   string
	Fee    int
	Active bool
	Sort   int
}

const zoneCols = `id, name, fee, active, sort`

func (s *Store) ListZones(ctx context.Context, activeOnly bool) ([]Zone, error) {
	rows, err := s.db.Query(ctx, `select `+zoneCols+` from delivery_zones where (not $1 or active) order by sort, name`, activeOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Zone])
}

func (s *Store) GetZone(ctx context.Context, id int64) (Zone, error) {
	rows, _ := s.db.Query(ctx, `select `+zoneCols+` from delivery_zones where id = $1`, id)
	z, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Zone])
	return z, mapErr(err)
}

func (s *Store) CreateZone(ctx context.Context, z Zone) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into delivery_zones (name, fee, active, sort) values ($1, $2, $3, $4) returning id`,
		z.Name, z.Fee, z.Active, z.Sort).Scan(&id)
	return id, mapErr(err)
}

func (s *Store) UpdateZone(ctx context.Context, z Zone) error {
	tag, err := s.db.Exec(ctx, `update delivery_zones set name = $2, fee = $3, active = $4, sort = $5 where id = $1`,
		z.ID, z.Name, z.Fee, z.Active, z.Sort)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteZone(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `delete from delivery_zones where id = $1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
