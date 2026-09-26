package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strconv"
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
	u, err := a.st.GetUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return "sess=" + a.tok.EncodeSession(userID, time.Now().Add(sessionTTL), u.PasswordHash)
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
	reg := do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Ana", "correct horse")))
	before := "sess=" + cookieHeader(reg, "sess")
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
	// Resetting is how someone locks out whoever else has their account, so
	// it has to end every session opened under the old password.
	if do(t, a, "GET", "/account", nil, "Cookie", before).Code == 200 {
		t.Fatal("a session from before the reset still works after it")
	}
	if do(t, a, "GET", "/account", nil, "Cookie", "sess="+cookieHeader(w, "sess")).Code != 200 {
		t.Fatal("the session the reset itself issued does not work")
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

// TestResetPageCanonicalDoesNotLeakToken is the M4 fix: the reset page's
// canonical/og:url used to default to BASE_URL + the request path, and for
// /reset/<token> the path IS the one-hour credential. Confirmed by
// execution in the review. Neither tag may contain the token, and both must
// point at /forgot instead.
func TestResetPageCanonicalDoesNotLeakToken(t *testing.T) {
	a, st := newDBApp(t)
	u, err := st.CreateUser(context.Background(), "leak@example.com", "x", "Ana", "customer")
	if err != nil {
		t.Fatal(err)
	}
	tok := a.tok.ResetToken(u.ID, time.Now().Add(time.Hour), u.PasswordHash)
	w := do(t, a, "GET", "/reset/"+tok, nil)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("reset page: %d", w.Code)
	}
	if strings.Contains(body, tok) {
		t.Fatalf("reset page must not leak the token anywhere in the body, found it in:\n%s", body)
	}
	want := a.cfg.BaseURL + "/forgot"
	if !strings.Contains(body, `rel="canonical" href="`+want+`"`) {
		t.Fatalf("canonical should point to %q, not the token URL:\n%s", want, body)
	}
	if !strings.Contains(body, `property="og:url" content="`+want+`"`) {
		t.Fatalf("og:url should point to %q, not the token URL:\n%s", want, body)
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

// TestSafeNext exercises safeNext directly rather than through HTTP: Go's
// client-side header parser drops a malformed Location header before an
// httptest assertion could ever see it, so an HTTP-level test provably
// cannot catch the ASCII-tab bypass (next=/%09/evil.com, which decodes to
// "/\t/evil.com" by the time it reaches safeNext).
func TestSafeNext(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"same-site path accepted", "/account", "/account"},
		{"same-site path with query accepted", "/products?x=1", "/products?x=1"},
		{"decoded ASCII tab rejected", "/\t/evil.com", ""},
		{"protocol-relative rejected", "//evil.com", ""},
		{"backslash bypass rejected", "/\\evil.com", ""},
		{"absolute https rejected", "https://evil.com", ""},
		{"absolute http rejected", "http://evil.com", ""},
		{"decoded newline rejected", "/\n/evil.com", ""},
		{"decoded carriage return rejected", "/\r/evil.com", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := safeNext(c.in); got != c.want {
				t.Fatalf("safeNext(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// longPassword is 30 Bengali runes: 90 UTF-8 bytes, well over bcrypt's
// 72-byte cap, while its rune count (30) stays comfortably under any
// rune-based limit — the exact shape that slipped past the old
// rune-only-bounded validPassword and reached bcrypt.GenerateFromPassword,
// which returned ErrPasswordTooLong and 500'd.
var longPassword = strings.Repeat("অ", 30)

func TestRegisterAndResetPasswordOverByteCap(t *testing.T) {
	a, st := newDBApp(t)

	w := do(t, a, "POST", "/register", strings.NewReader(registerForm("longpw@example.com", "Long", longPassword)))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "72 bytes") {
		t.Fatalf("long password on register: %d %s", w.Code, w.Body.String())
	}

	u, err := st.CreateUser(context.Background(), "reset-longpw@example.com", "x", "Ana", "customer")
	if err != nil {
		t.Fatal(err)
	}
	tok := a.tok.ResetToken(u.ID, time.Now().Add(time.Hour), u.PasswordHash)
	w = do(t, a, "POST", "/reset/"+tok, strings.NewReader(url.Values{"password": {longPassword}}.Encode()))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "72 bytes") {
		t.Fatalf("long password on reset: %d %s", w.Code, w.Body.String())
	}
}

func passwordForm(current, next string) *strings.Reader {
	return strings.NewReader(url.Values{"current_password": {current}, "password": {next}}.Encode())
}

func TestChangePasswordSignsOutOtherDevices(t *testing.T) {
	a, _ := newDBApp(t)
	do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Ana", "correct horse")))
	signIn := func(pw string) string {
		t.Helper()
		w := do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", pw)))
		if w.Code != 303 {
			t.Fatalf("login with %q: %d", pw, w.Code)
		}
		return "sess=" + cookieHeader(w, "sess")
	}
	signedIn := func(cookie string) bool {
		return do(t, a, "GET", "/account", nil, "Cookie", cookie).Code == 200
	}
	phone, laptop := signIn("correct horse"), signIn("correct horse")

	if w := do(t, a, "POST", "/account/password", passwordForm("a guess", "brand new pass"), "Cookie", phone); w.Code != 422 || !strings.Contains(w.Body.String(), "your current password") {
		t.Fatalf("wrong current password must be rejected: %d", w.Code)
	}
	if w := do(t, a, "POST", "/account/password", passwordForm("correct horse", "short"), "Cookie", phone); w.Code != 422 || !strings.Contains(w.Body.String(), "at least 8") {
		t.Fatalf("too-short new password must be rejected: %d", w.Code)
	}
	if !signedIn(laptop) {
		t.Fatal("a rejected change must not sign anyone out")
	}

	w := do(t, a, "POST", "/account/password", passwordForm("correct horse", "brand new pass"), "Cookie", phone)
	if w.Code != 303 {
		t.Fatalf("change password: %d", w.Code)
	}
	if !signedIn("sess=" + cookieHeader(w, "sess")) {
		t.Fatal("the device that changed the password must stay signed in")
	}
	if signedIn(laptop) {
		t.Fatal("another device's session survived the password change")
	}
	if signedIn(phone) {
		t.Fatal("this device's pre-change cookie survived the password change")
	}
	if w := do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "correct horse"))); w.Code == 303 {
		t.Fatal("the old password still logs in")
	}
	signIn("brand new pass")
}

// A stolen session cookie must not become a permanent takeover by guessing
// the current password until it changes the password.
func TestChangePasswordIsRateLimited(t *testing.T) {
	a, _ := newDBApp(t)
	if w := do(t, a, "POST", "/account/password", passwordForm("x", "brand new pass")); w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Fatalf("signed-out change must redirect to login: %d %q", w.Code, w.Header().Get("Location"))
	}
	reg := do(t, a, "POST", "/register", strings.NewReader(registerForm("ana@example.com", "Ana", "correct horse")))
	c := "sess=" + cookieHeader(reg, "sess")
	for i := 0; i < 5; i++ {
		do(t, a, "POST", "/account/password", passwordForm("guess "+strconv.Itoa(i), "brand new pass"), "Cookie", c)
	}
	w := do(t, a, "POST", "/account/password", passwordForm("correct horse", "brand new pass"), "Cookie", c)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Too many attempts") {
		t.Fatalf("sixth attempt, even with the right password, must be refused: %d", w.Code)
	}
	if w := do(t, a, "POST", "/login", strings.NewReader(loginForm("ana@example.com", "correct horse"))); w.Code != 303 {
		t.Fatal("a refused change must leave the password as it was")
	}
}
