package token

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

var s = New([]byte(strings.Repeat("k", 32)))

func TestCartRoundTrip(t *testing.T) {
	in := []CartLine{{VariantID: 7, Qty: 2}, {VariantID: 9, Qty: 1}}
	out := s.DecodeCart(s.EncodeCart(in))
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("got %+v", out)
	}
}

func TestCartTamperAndGarbage(t *testing.T) {
	v := s.EncodeCart([]CartLine{{VariantID: 7, Qty: 2}})
	if s.DecodeCart(v[:len(v)-2]+"zz") != nil {
		t.Fatal("tampered signature accepted")
	}
	if s.DecodeCart("not-a-cookie") != nil || s.DecodeCart("") != nil {
		t.Fatal("garbage accepted")
	}
	other := New([]byte(strings.Repeat("x", 32)))
	if other.DecodeCart(v) != nil {
		t.Fatal("wrong key accepted")
	}
}

func TestCartCaps(t *testing.T) {
	var in []CartLine
	for i := 1; i <= 25; i++ {
		in = append(in, CartLine{VariantID: int64(i), Qty: 1})
	}
	in = append(in, CartLine{VariantID: 99, Qty: 0}, CartLine{VariantID: 98, Qty: 100}, CartLine{VariantID: 0, Qty: 1})
	out := s.DecodeCart(s.EncodeCart(in))
	if len(out) != MaxCartLines {
		t.Fatalf("want %d lines, got %d", MaxCartLines, len(out))
	}
	for _, l := range out {
		if l.Qty < 1 || l.Qty > 99 || l.VariantID <= 0 {
			t.Fatalf("invalid line kept: %+v", l)
		}
	}
}

func TestCartFiltersInvalidLines(t *testing.T) {
	in := []CartLine{
		{VariantID: 0, Qty: 1}, {VariantID: -1, Qty: 1},
		{VariantID: 1, Qty: -1}, {VariantID: 1, Qty: 0}, {VariantID: 1, Qty: 100},
		{VariantID: 7, Qty: 1}, {VariantID: 9, Qty: 99},
	}
	out := s.DecodeCart(s.EncodeCart(in))
	if len(out) != 2 || out[0] != in[5] || out[1] != in[6] {
		t.Fatalf("got %+v", out)
	}
	if out := s.DecodeCart(s.EncodeCart(nil)); out != nil {
		t.Fatalf("nil cart decoded as %+v", out)
	}
	if out := s.DecodeCart(s.EncodeCart([]CartLine{})); len(out) != 0 {
		t.Fatalf("empty cart decoded as %+v", out)
	}
}

func TestCartMalformedPayload(t *testing.T) {
	for _, enc := range []string{
		"!", base64.RawURLEncoding.EncodeToString([]byte("not-json")),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"q":1}`)),
	} {
		if out := s.DecodeCart(enc + "." + s.sign(enc)); out != nil {
			t.Fatalf("malformed payload %q accepted: %+v", enc, out)
		}
	}
}

// hashIs is a lookupHash that reports the user's current password hash as h.
func hashIs(h string) func(int64) (string, bool) {
	return func(int64) (string, bool) { return h, true }
}

func TestSession(t *testing.T) {
	now := time.Unix(1700000000, 0)
	v := s.EncodeSession(42, now.Add(time.Hour), "hash-a")
	if id, ok := s.DecodeSession(v, now, hashIs("hash-a")); !ok || id != 42 {
		t.Fatalf("got %d %v", id, ok)
	}
	if _, ok := s.DecodeSession(v, now.Add(2*time.Hour), hashIs("hash-a")); ok {
		t.Fatal("expired session accepted")
	}
	if _, ok := s.DecodeSession("1.2.3", now, hashIs("hash-a")); ok {
		t.Fatal("bad signature accepted")
	}
	if _, ok := s.DecodeSession(v, now, func(int64) (string, bool) { return "", false }); ok {
		t.Fatal("session accepted for a user that no longer exists")
	}
}

// A password change must sign out every session issued under the old hash.
func TestSessionDiesWithPasswordChange(t *testing.T) {
	now := time.Unix(1700000000, 0)
	v := s.EncodeSession(42, now.Add(time.Hour), "old-hash")
	if _, ok := s.DecodeSession(v, now, hashIs("new-hash")); ok {
		t.Fatal("session signed over the old password hash still accepted after the password changed")
	}
}

// Sessions and reset tokens share the id.exp.sig shape and are both signed
// over the password hash, so without domain separation each would verify as
// the other: a leaked reset link would be a 30-day login cookie.
func TestSessionAndResetTokenAreNotInterchangeable(t *testing.T) {
	now := time.Unix(1700000000, 0)
	reset := s.ResetToken(42, now.Add(time.Hour), "hash-a")
	if _, ok := s.DecodeSession(reset, now, hashIs("hash-a")); ok {
		t.Fatal("a password-reset token was accepted as a session cookie")
	}
	sess := s.EncodeSession(42, now.Add(time.Hour), "hash-a")
	if _, ok := s.ParseReset(sess, now, hashIs("hash-a")); ok {
		t.Fatal("a session cookie was accepted as a password-reset token")
	}
}

func TestSessionExpiryBoundary(t *testing.T) {
	exp := time.Unix(1700000000, 0)
	v := s.EncodeSession(42, exp, "hash-a")
	if id, ok := s.DecodeSession(v, exp.Add(-time.Nanosecond), hashIs("hash-a")); !ok || id != 42 {
		t.Fatalf("unexpired session rejected: %d %v", id, ok)
	}
	if _, ok := s.DecodeSession(v, exp, hashIs("hash-a")); ok {
		t.Fatal("session accepted at expiry")
	}
}

func TestSessionInvalid(t *testing.T) {
	now := time.Unix(1700000000, 0)
	// Correctly signed, but the id or expiry itself is invalid.
	for _, msg := range []string{"x.1700003600", "9223372036854775808.1700003600", "1.x", "1.9223372036854775808", "0.1700003600", "-1.1700003600"} {
		if id, ok := s.DecodeSession(msg+"."+s.sign("sess:"+msg+".hash-a"), now, hashIs("hash-a")); ok || id != 0 {
			t.Fatalf("invalid session %q accepted: %d %v", msg, id, ok)
		}
	}
	v := s.EncodeSession(42, now.Add(time.Hour), "hash-a")
	for _, tok := range []string{"", "1.2", v + ".extra", "43" + strings.TrimPrefix(v, "42")} {
		if id, ok := s.DecodeSession(tok, now, hashIs("hash-a")); ok || id != 0 {
			t.Fatalf("malformed or tampered session %q accepted: %d %v", tok, id, ok)
		}
	}
	other := New([]byte(strings.Repeat("x", 32)))
	if _, ok := other.DecodeSession(v, now, hashIs("hash-a")); ok {
		t.Fatal("wrong key accepted")
	}
}

func TestReset(t *testing.T) {
	now := time.Unix(1700000000, 0)
	hash := "$2a$12$abcdefghijklmnopqrstuv"
	tok := s.ResetToken(5, now.Add(time.Hour), hash)
	lookup := func(id int64) (string, bool) { return hash, id == 5 }
	if id, ok := s.ParseReset(tok, now, lookup); !ok || id != 5 {
		t.Fatalf("valid token rejected: %d %v", id, ok)
	}
	if _, ok := s.ParseReset(tok, now.Add(2*time.Hour), lookup); ok {
		t.Fatal("expired token accepted")
	}
	changed := func(int64) (string, bool) { return "$2a$12$zzzzzzzzzzzzzzzzzzzzzz", true }
	if _, ok := s.ParseReset(tok, now, changed); ok {
		t.Fatal("token survived password change")
	}
}

func TestResetPasswordHashSuffixChange(t *testing.T) {
	now := time.Unix(1700000000, 0)
	hash := "$2a$12$abcdefghijklmnopqrstuv"
	tok := s.ResetToken(5, now.Add(time.Hour), hash)
	changed := func(int64) (string, bool) { return hash[:12] + "changed", true }
	if _, ok := s.ParseReset(tok, now, changed); ok {
		t.Fatal("token survived a password hash change after its prefix")
	}
}

func TestResetExpiryBoundary(t *testing.T) {
	exp := time.Unix(1700000000, 0)
	hash := "$2a$12$abcdefghijklmnopqrstuv"
	tok := s.ResetToken(5, exp, hash)
	lookup := func(id int64) (string, bool) { return hash, id == 5 }
	if id, ok := s.ParseReset(tok, exp.Add(-time.Nanosecond), lookup); !ok || id != 5 {
		t.Fatalf("unexpired token rejected: %d %v", id, ok)
	}
	if _, ok := s.ParseReset(tok, exp, lookup); ok {
		t.Fatal("reset token accepted at expiry")
	}
}

func TestResetInvalid(t *testing.T) {
	now := time.Unix(1700000000, 0)
	hash := "$2a$12$abcdefghijklmnopqrstuv"
	lookup := func(id int64) (string, bool) { return hash, id == 5 }
	tok := s.ResetToken(5, now.Add(time.Hour), hash)
	for _, bad := range []string{"", "1.2", tok + ".extra", "x.1700003600.sig", "9223372036854775808.1700003600.sig", "5.x.sig", "5.9223372036854775808.sig", tok[:len(tok)-2] + "zz", "6" + strings.TrimPrefix(tok, "5")} {
		if id, ok := s.ParseReset(bad, now, lookup); ok || id != 0 {
			t.Fatalf("invalid reset token %q accepted: %d %v", bad, id, ok)
		}
	}
	for _, id := range []int64{0, -1} {
		bad := s.ResetToken(id, now.Add(time.Hour), hash)
		if _, ok := s.ParseReset(bad, now, func(int64) (string, bool) {
			t.Error("invalid user ID looked up")
			return hash, true
		}); ok {
			t.Fatalf("invalid user ID %d accepted", id)
		}
	}
	if _, ok := s.ParseReset(tok, now, func(int64) (string, bool) { return "", false }); ok {
		t.Fatal("missing user accepted")
	}
	other := New([]byte(strings.Repeat("x", 32)))
	if _, ok := other.ParseReset(tok, now, lookup); ok {
		t.Fatal("wrong key accepted")
	}
}

func TestOrderToken(t *testing.T) {
	tok := s.OrderToken("AV-001001")
	if len(tok) != 16 || !s.VerifyOrderToken("AV-001001", tok) {
		t.Fatalf("bad token %q", tok)
	}
	if _, err := hex.DecodeString(tok); err != nil {
		t.Fatalf("token is not hex: %v", err)
	}
	if s.VerifyOrderToken("AV-001002", tok) {
		t.Fatal("token valid for another order")
	}
	for _, bad := range []string{"", tok[:15], tok + "0", tok[:14] + "zz"} {
		if s.VerifyOrderToken("AV-001001", bad) {
			t.Fatalf("invalid order token %q accepted", bad)
		}
	}
	other := New([]byte(strings.Repeat("x", 32)))
	if other.VerifyOrderToken("AV-001001", tok) {
		t.Fatal("wrong key accepted")
	}
}
