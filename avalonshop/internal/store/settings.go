package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Setting returns the stored value for key, or "" when it was never set.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRow(ctx, `select value from settings where key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.Exec(ctx, `insert into settings (key, value) values ($1, $2)
		on conflict (key) do update set value = excluded.value`, key, value)
	return err
}
