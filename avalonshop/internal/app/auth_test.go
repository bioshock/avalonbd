package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"avalonshop/internal/mail"
	"avalonshop/internal/store"
)

func registerForm(email, name, pw string) string {
	return url.Values{"email": {email}, "name": {name}, "password": {pw}}.Encode()
}

func loginForm(email, pw string) string {
	return url.Values{"email": {email}, "password": {pw}}.Encode()
}

func sessionCookie(t *testing.T, a *App, userID int64) string {
	t.Helper()
	return "sess=" + a.tok.EncodeSession(userID, time.Now().Add(sessionTTL))
}

func TestRegisterLoginLogout(t *testing.T) {
	a, _ := newDBApp(t)
	w := do(t, a, "POST", "/register", strings.NewReader(registerForm("Ana@Example.com", "Ana", "short")))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "at least 8") {
		t.Fatalf("weak password: %d", w.Code)
	}
	w = do(t, a, "POST", "/register", strings.NewReader(registerForm("Ana@Example.com", "Ana", "correct horse")))
	if w.Code != 303 || w.Header().Get("Location") != "/account" || cookieHeader(w, "sess") == "" {
		t.Fatalf("register: %d %s", w.Code, w.Header().Get("Location"))
	}
	sess := cookieHeader(w, "sess")
	w = do(t, a, "GET", "/account", nil, "Cookie", "sess="+sess)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Ana") {
		t.Fatalf("account page: %d", w.Code)
	}
	w = do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Dup", "correct horse")))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "already registered") {
		t.Fatalf("duplicate: %d", w.Code)
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "wrong password")))
	if w.Code != 401 || !strings.Contains(w.Body.String(), "Wrong email or password") {
		t.Fatalf("bad login: %d", w.Code)
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ANA@example.com", "correct horse")+"&next=/checkout"))
	if w.Code != 303 || w.Header().Get("Location") != "/checkout" || cookieHeader(w, "sess") == "" {
		t.Fatalf("good login: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "correct horse")+"&next=//evil.com"))
	if w.Header().Get("Location") != "/account" {
		t.Fatalf("open redirect: %s", w.Header().Get("Location"))
	}
	w = do(t, a, "POST", "/logout", nil, "Cookie", "sess="+sess)
	if w.Code != 303 {
		t.Fatal("logout")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "sess" && c.MaxAge != -1 {
			t.Fatal("sess cookie not cleared")
		}
	}
	if w := do(t, a, "GET", "/account", nil); w.Code != 303 || w.Header().Get("Location") != "/login?next=%2Faccount" {
		t.Fatalf("logged-out account: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestLoginRateLimitAndAdminRedirect(t *testing.T) {
	a, st := newDBApp(t)
	st.SeedAdmin(context.Background(), "admin@example.com", "admin-pass-123")
	for i := 0; i < 5; i++ {
		do(t, a, "POST", "/login", strings.NewReader(loginForm("admin@example.com", "nope")))
	}
	w := do(t, a, "POST", "/login", strings.NewReader(loginForm("admin@example.com", "admin-pass-123")))
	if w.Code != 429 {
		t.Fatalf("6th attempt should be blocked even with the right password, got %d", w.Code)
	}
	a.loginLimit = newLimiter(5, 15*time.Minute)
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("admin@example.com", "admin-pass-123")))
	if w.Code != 303 || w.Header().Get("Location") != "/admin" {
		t.Fatalf("admin should land on /admin: %d %s", w.Code, w.Header().Get("Location"))
	}
}

var resetRe = regexp.MustCompile(`/reset/([0-9]+\.[0-9]+\.[0-9a-f]+)`)

func TestForgotAndReset(t *testing.T) {
	a, _ := newDBApp(t)
	var logbuf bytes.Buffer
	m, _ := mail.New("", "587", "", "", "shop@test.local", os.DirFS("../.."), slog.New(slog.NewTextHandler(&logbuf, nil)))
	a.mail = m
	do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Ana", "correct horse")))
	w := do(t, a, "POST", "/forgot", strings.NewReader("email=nobody@example.com"))
	if w.Code != 303 || cookieHeader(w, "flash") == "" {
		t.Fatalf("unknown email must look identical: %d", w.Code)
	}
	w = do(t, a, "POST", "/forgot", strings.NewReader("email=ANA@example.com"))
	if w.Code != 303 {
		t.Fatal("forgot")
	}
	a.mail.Wait()
	m2 := resetRe.FindStringSubmatch(logbuf.String())
	if m2 == nil {
		t.Fatalf("no reset link in dev mail log:\n%s", logbuf.String())
	}
	tok := m2[1]
	w = do(t, a, "GET", "/reset/"+tok, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "New password") {
		t.Fatalf("reset form: %d", w.Code)
	}
	w = do(t, a, "POST", "/reset/"+tok, strings.NewReader("password=new password 9"))
	if w.Code != 303 || cookieHeader(w, "sess") == "" {
		t.Fatalf("reset post: %d", w.Code)
	}
	if w := do(t, a, "GET", "/reset/"+tok, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatal("token should be dead after use")
	}
	w = do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "new password 9")))
	if w.Code != 303 {
		t.Fatal("login with new password failed")
	}
	if w := do(t, a, "GET", "/reset/garbage", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatal("garbage token")
	}
}

func TestAccountProfileAndOrders(t *testing.T) {
	a, st := newDBApp(t)
	u, _ := st.CreateUser(context.Background(), "ana@example.com", "x", "Ana", "customer")
	sess := sessionCookie(t, a, u.ID)
	w := do(t, a, "POST", "/account", strings.NewReader(url.Values{"name": {"Ana B"}, "phone": {"+88 01712345678"}, "address": {"Road 1, Rajshahi"}}.Encode()), "Cookie", sess)
	if w.Code != 303 {
		t.Fatalf("profile save: %d", w.Code)
	}
	got, _ := st.GetUser(context.Background(), u.ID)
	if got.Name != "Ana B" || got.Phone != "01712345678" {
		t.Fatalf("profile not saved: %+v", got)
	}
	w = do(t, a, "POST", "/account", strings.NewReader(url.Values{"name": {"Ana"}, "phone": {"123"}, "address": {""}}.Encode()), "Cookie", sess)
	if w.Code != 422 {
		t.Fatalf("bad phone should 422, got %d", w.Code)
	}
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	zone, _ := st.CreateZone(context.Background(), store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	o, err := st.PlaceOrder(context.Background(), store.NewOrder{UserID: &u.ID, Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1", ZoneID: zone, Lines: []store.OrderLine{{VariantID: v1, Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	w = do(t, a, "GET", "/account", nil, "Cookie", sess)
	if !strings.Contains(w.Body.String(), o.Number) {
		t.Fatal("order history missing")
	}
	if w := do(t, a, "GET", "/orders/"+o.Number, nil, "Cookie", sess); w.Code != 200 {
		t.Fatalf("owner should see order without token, got %d", w.Code)
	}
}
