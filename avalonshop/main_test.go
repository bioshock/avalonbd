package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

// TestSeedAdminOrWarnDoesNotFailBootOnDuplicateEmail is the C3 fix: main.go
// used to propagate SeedAdmin's error straight out of run(), which main()
// logged as "fatal" and exited on — Coolify then restarts with the same
// ADMIN_EMAIL forever. This reproduces the reachable trigger: an owner
// registers through the shop with their own address, then later adds
// ADMIN_EMAIL set to that same address. seedAdminOrWarn must log the failure
// and let the caller keep booting, not return an error itself.
func TestSeedAdminOrWarnDoesNotFailBootOnDuplicateEmail(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "owner@example.com", "hash", "Owner", "customer"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	seedAdminOrWarn(ctx, st, log, "owner@example.com", "some-password")

	if !strings.Contains(buf.String(), "seed admin failed") {
		t.Fatalf("expected the failure to be logged, got: %s", buf.String())
	}
	// Boot must continue with the existing user untouched and no admin
	// seeded — that's the accepted cost, not a crash.
	u, err := st.GetUserByEmail(ctx, "owner@example.com")
	if err != nil || u.Role != "customer" {
		t.Fatalf("existing user should be unaffected: %+v %v", u, err)
	}
}

// TestSeedAdminOrWarnDoesNotFailBootOnOverLongPassword covers the other C3
// trigger: an 80+ byte ADMIN_PASSWORD, very reachable with a Bangla
// passphrase (3 bytes per rune in UTF-8), must not crash the very first
// deploy either.
func TestSeedAdminOrWarnDoesNotFailBootOnOverLongPassword(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	longPass := strings.Repeat("অ", 30) // 90 bytes in UTF-8
	seedAdminOrWarn(ctx, st, log, "admin@example.com", longPass)

	if !strings.Contains(buf.String(), "seed admin failed") {
		t.Fatalf("expected the failure to be logged, got: %s", buf.String())
	}
	if _, err := st.GetUserByEmail(ctx, "admin@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("admin should not have been created with a rejected password")
	}
}
