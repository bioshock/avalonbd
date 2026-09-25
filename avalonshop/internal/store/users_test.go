package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"

	"golang.org/x/crypto/bcrypt"
)

func TestUsers(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "Ana@Example.com", "hash1", "Ana", "customer")
	if err != nil || u.ID == 0 || u.Role != "customer" {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := st.CreateUser(ctx, "ana@example.com", "x", "Dup", "customer"); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	got, err := st.GetUserByEmail(ctx, "ANA@example.com")
	if err != nil || got.ID != u.ID {
		t.Fatalf("case-insensitive lookup failed: %v", err)
	}
	if err := st.UpdateProfile(ctx, u.ID, "Ana B", "01712345678", "Road 1, Rajshahi"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePassword(ctx, u.ID, "hash2"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetUser(ctx, u.ID)
	if got.Name != "Ana B" || got.Phone != "01712345678" || got.PasswordHash != "hash2" {
		t.Fatalf("updates lost: %+v", got)
	}
	if _, err := st.GetUser(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSeedAdmin(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	if err := st.SeedAdmin(ctx, "", ""); err != nil {
		t.Fatal("empty seed should be a no-op")
	}
	if err := st.SeedAdmin(ctx, "admin@example.com", "secret123"); err != nil {
		t.Fatal(err)
	}
	a, err := st.GetUserByEmail(ctx, "admin@example.com")
	if err != nil || a.Role != "admin" || bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte("secret123")) != nil {
		t.Fatalf("admin not seeded correctly: %+v %v", a, err)
	}
	if err := st.SeedAdmin(ctx, "second@example.com", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUserByEmail(ctx, "second@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("second admin should not be seeded once one exists")
	}
}

// TestSeedAdminRejectsOverLongPasswordEarly is half of the C3 fix: an
// ADMIN_PASSWORD over bcrypt's 72-byte limit is very reachable, since Bangla
// is 3 bytes per rune in UTF-8 — a 24-character Bangla passphrase already
// exceeds it. SeedAdmin must reject it itself, with a message naming the
// limit, rather than letting bcrypt's raw "password length exceeds 72 bytes"
// surface, and it must not create a half-seeded admin row.
func TestSeedAdminRejectsOverLongPasswordEarly(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	longPass := strings.Repeat("অ", 30) // 90 bytes in UTF-8, well over 72
	err := st.SeedAdmin(ctx, "admin@example.com", longPass)
	if err == nil {
		t.Fatal("expected an error for an over-long ADMIN_PASSWORD")
	}
	if !strings.Contains(err.Error(), "72") || !strings.Contains(err.Error(), "Bangla") {
		t.Fatalf("error should name the 72-byte limit and the Bangla-bytes reason, got: %v", err)
	}
	if strings.Contains(err.Error(), "bcrypt:") {
		t.Fatalf("the raw bcrypt error must not surface: %v", err)
	}
	if _, err := st.GetUserByEmail(ctx, "admin@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("no admin should have been created with a rejected password")
	}
}
