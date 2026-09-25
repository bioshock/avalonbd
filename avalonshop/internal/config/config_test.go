package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func good() map[string]string {
	return map[string]string{
		"DATABASE_URL":       "postgres://x",
		"BASE_URL":           "https://shop.example.com/",
		"SESSION_SECRET":     strings.Repeat("ab", 32),
		"MAIL_FROM":          "shop@example.com",
		"ORDER_NOTIFY_EMAIL": "owner@example.com",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(good()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.SMTPPort != "587" || c.UploadDir != "./data/uploads" {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.BaseURL != "https://shop.example.com" {
		t.Fatalf("trailing slash not trimmed: %q", c.BaseURL)
	}
	if !c.Secure() || len(c.SessionSecret) != 32 {
		t.Fatalf("secure=%v secretlen=%d", c.Secure(), len(c.SessionSecret))
	}
}

func TestLoadMissing(t *testing.T) {
	m := good()
	delete(m, "DATABASE_URL")
	if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
	m = good()
	m["SESSION_SECRET"] = "abcd"
	if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("expected SESSION_SECRET error, got %v", err)
	}
}

// TestLoadMailFromMustBeBareAddress is the C4 fix: a display-name form of
// MAIL_FROM — exactly what Brevo's Senders screen shows and the natural
// thing to paste — must be rejected at boot, not discovered when the first
// order's email silently fails to send.
func TestLoadMailFromMustBeBareAddress(t *testing.T) {
	m := good()
	m["MAIL_FROM"] = "Avalon Corporation <shop@example.com>"
	if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), "MAIL_FROM") {
		t.Fatalf("expected a MAIL_FROM error for a display-name address, got %v", err)
	}
}

func TestLoadDotEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# comment\nFOO_TEST=bar\nQUOTED_TEST=\"a b\"\n\n"), 0o600)
	t.Setenv("FOO_TEST", "")
	os.Unsetenv("FOO_TEST")
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FOO_TEST") != "bar" || os.Getenv("QUOTED_TEST") != "a b" {
		t.Fatalf("got %q %q", os.Getenv("FOO_TEST"), os.Getenv("QUOTED_TEST"))
	}
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing file should be fine: %v", err)
	}
}
