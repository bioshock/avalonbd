package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string `db:"password_hash"`
	Name         string
	Phone        string
	Address      string
	Role         string
	CreatedAt    time.Time `db:"created_at"`
}

const userCols = `id, email, password_hash, name, phone, address, role, created_at`

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, name, role string) (User, error) {
	rows, _ := s.db.Query(ctx, `insert into users (email, password_hash, name, role) values ($1, $2, $3, $4) returning `+userCols,
		email, passwordHash, name, role)
	u, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[User])
	return u, mapErr(err)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	rows, _ := s.db.Query(ctx, `select `+userCols+` from users where lower(email) = lower($1)`, email)
	u, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[User])
	return u, mapErr(err)
}

func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	rows, _ := s.db.Query(ctx, `select `+userCols+` from users where id = $1`, id)
	u, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[User])
	return u, mapErr(err)
}

func (s *Store) UpdateProfile(ctx context.Context, id int64, name, phone, address string) error {
	tag, err := s.db.Exec(ctx, `update users set name = $2, phone = $3, address = $4 where id = $1`, id, name, phone, address)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, hash string) error {
	tag, err := s.db.Exec(ctx, `update users set password_hash = $2 where id = $1`, id, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// bcryptMaxPasswordBytes is bcrypt.GenerateFromPassword's hard limit: it
// errors on any input longer than this. Checking it before calling bcrypt
// turns a cryptic "bcrypt: password length exceeds 72 bytes" into a message
// that names the limit and the reason it's easy to hit by accident (C3).
const bcryptMaxPasswordBytes = 72

// SeedAdmin creates the first admin from env on first boot. Does nothing if
// email or password is empty, or if any admin already exists. The caller
// (main.go) must not treat a returned error as fatal to the whole server:
// a seeding failure (a duplicate email, or an over-long password) must not
// crash-loop the storefront (C3).
func (s *Store) SeedAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	if len(password) > bcryptMaxPasswordBytes {
		return fmt.Errorf("ADMIN_PASSWORD is %d bytes, over bcrypt's %d-byte limit; note that Bangla characters are 3 bytes each in UTF-8, so a short Bangla passphrase can already be over the limit — shorten ADMIN_PASSWORD and redeploy", len(password), bcryptMaxPasswordBytes)
	}
	var n int
	if err := s.db.QueryRow(ctx, `select count(*) from users where role = 'admin'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	_, err = s.CreateUser(ctx, email, string(hash), "Admin", "admin")
	return err
}
