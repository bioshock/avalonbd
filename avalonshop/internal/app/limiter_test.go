package app

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th call should be blocked")
	}
	if !l.Allow("b") {
		t.Fatal("other key unaffected")
	}
	l.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if !l.Allow("a") {
		t.Fatal("window should have expired")
	}
	f := newLimiter(2, time.Minute)
	if f.Blocked("x") {
		t.Fatal("fresh key must not be blocked")
	}
	f.Hit("x")
	f.Hit("x")
	if !f.Blocked("x") {
		t.Fatal("two hits should block at limit 2")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	if clientIP(r) != "10.0.0.1" {
		t.Fatal(clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	if clientIP(r) != "203.0.113.5" {
		t.Fatal(clientIP(r))
	}
}
