package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"avalonshop/internal/store"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// dummyHash keeps login timing uniform when the email is unknown. It is
// computed once and cached. A swallowed error here would cache an empty
// string forever: bcrypt.CompareHashAndPassword("") fails in microseconds
// instead of doing real work, turning the unknown-email path into a timing
// oracle on exactly the property spec §11 asks for — so a generation failure
// panics loudly instead of silently degrading to an instant compare.
var dummyHash = sync.OnceValue(func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("avalon-dummy-password"), bcryptCost)
	if err != nil {
		panic("auth: failed to generate dummy bcrypt hash: " + err.Error())
	}
	return string(h)
})

type authForm struct {
	Email, Name, Next string
	Errors            map[string]string
	Invalid           bool // reset: token bad or expired
}

// safeNext only allows same-site relative paths. It parses (not just prefix-
// matches) the input so control characters — an ASCII tab, CR or LF, which a
// browser's URL parser silently strips before resolving the redirect
// (turning "/\t/evil.com" into "//evil.com") — are rejected outright by
// url.Parse instead of slipping past a prefix check. The "//" and "/\"
// prefix checks stay alongside it: browsers treat a leading backslash as a
// path separator for special schemes, so "/\evil.com" also resolves as
// protocol-relative, and net/url (unlike a browser's WHATWG parser) does not
// flag that on its own.
func safeNext(s string) string {
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return ""
	}
	return s
}

func (a *App) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}

func (a *App) authPage(w http.ResponseWriter, r *http.Request, status int, name, title string, f authForm) {
	a.renderStatus(w, r, status, "store/"+name+".html", page{Title: title, NoIndex: true, Data: f})
}

func (a *App) loginGet(w http.ResponseWriter, r *http.Request) {
	if a.currentUser(r) != nil {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	a.authPage(w, r, http.StatusOK, "login", "Log in", authForm{Next: safeNext(r.URL.Query().Get("next")), Errors: map[string]string{}})
}

func (a *App) loginPost(w http.ResponseWriter, r *http.Request) {
	f := authForm{Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), Next: safeNext(r.FormValue("next")), Errors: map[string]string{}}
	// Rate-limit by IP or email (not the pair): spec §7 calls for 5 failed
	// attempts per email-or-IP per 15 minutes. Keying on the combined pair
	// would let an attacker rotate either half and never trip the limit.
	ipKey := "ip:" + clientIP(r)
	emailKey := "email:" + f.Email
	if a.loginLimit.Blocked(ipKey) || a.loginLimit.Blocked(emailKey) {
		f.Errors["form"] = "Too many attempts. Please try again in 15 minutes."
		a.authPage(w, r, http.StatusTooManyRequests, "login", "Log in", f)
		return
	}
	u, err := a.st.GetUserByEmail(r.Context(), f.Email)
	hash := dummyHash()
	if err == nil {
		hash = u.PasswordHash
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.FormValue("password"))) != nil || err != nil {
		a.loginLimit.Hit(ipKey)
		if f.Email != "" {
			a.loginLimit.Hit(emailKey)
		}
		f.Errors["form"] = "Wrong email or password."
		a.authPage(w, r, http.StatusUnauthorized, "login", "Log in", f)
		return
	}
	a.login(w, u.ID)
	dest := f.Next
	if dest == "" {
		dest = "/account"
		if u.Role == "admin" {
			dest = "/admin"
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (a *App) logoutPost(w http.ResponseWriter, r *http.Request) {
	a.logout(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) registerGet(w http.ResponseWriter, r *http.Request) {
	a.authPage(w, r, http.StatusOK, "register", "Create account", authForm{Errors: map[string]string{}})
}

// validPassword bounds the minimum in runes and the maximum in bytes: bcrypt
// (golang.org/x/crypto/bcrypt) rejects any input over 72 bytes with
// ErrPasswordTooLong, and a multi-byte passphrase (Bangla, emoji, ...) can
// pass a rune-only minimum while still exceeding that byte cap.
const maxPasswordBytes = 72

var passwordLengthMsg = "Use at least 8 characters, and no more than 72 bytes — long non-Latin passphrases can hit that limit."

func validPassword(pw string) bool {
	return utf8.RuneCountInString(pw) >= 8 && len(pw) <= maxPasswordBytes
}

func (a *App) registerPost(w http.ResponseWriter, r *http.Request) {
	f := authForm{Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), Name: strings.TrimSpace(r.FormValue("name")), Errors: map[string]string{}}
	pw := r.FormValue("password")
	if l := utf8.RuneCountInString(f.Name); l < 1 || l > 100 {
		f.Errors["name"] = "Please enter your name."
	}
	if !validEmail(f.Email) {
		f.Errors["email"] = "Enter a valid email address."
	}
	if !validPassword(pw) {
		f.Errors["password"] = passwordLengthMsg
	}
	if len(f.Errors) > 0 {
		a.authPage(w, r, http.StatusUnprocessableEntity, "register", "Create account", f)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	u, err := a.st.CreateUser(r.Context(), f.Email, string(hash), f.Name, "customer")
	if errors.Is(err, store.ErrDuplicate) {
		f.Errors["email"] = "That email is already registered. Try logging in."
		a.authPage(w, r, http.StatusUnprocessableEntity, "register", "Create account", f)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.login(w, u.ID)
	a.setFlash(w, "Welcome to Avalon!")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (a *App) forgotGet(w http.ResponseWriter, r *http.Request) {
	a.authPage(w, r, http.StatusOK, "forgot", "Reset password", authForm{Errors: map[string]string{}})
}

func (a *App) forgotPost(w http.ResponseWriter, r *http.Request) {
	if !a.forgotLimit.Allow(clientIP(r)) {
		f := authForm{Errors: map[string]string{"form": "Too many attempts. Please try again in 15 minutes."}}
		a.authPage(w, r, http.StatusTooManyRequests, "forgot", "Reset password", f)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if u, err := a.st.GetUserByEmail(r.Context(), email); err == nil {
		tok := a.tok.ResetToken(u.ID, time.Now().Add(time.Hour), u.PasswordHash)
		a.mail.Send(u.Email, "password_reset", map[string]any{"Name": u.Name, "ResetURL": a.cfg.BaseURL + "/reset/" + tok})
	}
	a.setFlash(w, "If that email is registered, we've sent a reset link. Check your inbox.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) lookupHash(ctx context.Context) func(int64) (string, bool) {
	return func(id int64) (string, bool) {
		u, err := a.st.GetUser(ctx, id)
		if err != nil {
			return "", false
		}
		return u.PasswordHash, true
	}
}

func (a *App) resetGet(w http.ResponseWriter, r *http.Request) {
	_, ok := a.tok.ParseReset(r.PathValue("token"), time.Now(), a.lookupHash(r.Context()))
	a.authPage(w, r, http.StatusOK, "reset", "Choose a new password", authForm{Invalid: !ok, Errors: map[string]string{}})
}

func (a *App) resetPost(w http.ResponseWriter, r *http.Request) {
	id, ok := a.tok.ParseReset(r.PathValue("token"), time.Now(), a.lookupHash(r.Context()))
	if !ok {
		a.authPage(w, r, http.StatusOK, "reset", "Choose a new password", authForm{Invalid: true, Errors: map[string]string{}})
		return
	}
	pw := r.FormValue("password")
	if !validPassword(pw) {
		a.authPage(w, r, http.StatusUnprocessableEntity, "reset", "Choose a new password", authForm{Errors: map[string]string{"password": passwordLengthMsg}})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.st.UpdatePassword(r.Context(), id, string(hash)); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.login(w, id)
	a.setFlash(w, "Password updated.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

type accountData struct {
	User   *store.User
	Orders []store.Order
	Form   map[string]string
	Errors map[string]string
}

func (a *App) accountGet(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	orders, err := a.st.ListOrdersByUser(r.Context(), u.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "store/account.html", page{Title: "Your account", NoIndex: true, Data: accountData{
		User: u, Orders: orders, Form: map[string]string{"name": u.Name, "phone": u.Phone, "address": u.Address}, Errors: map[string]string{},
	}})
}

func (a *App) accountPost(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	form := map[string]string{"name": strings.TrimSpace(r.FormValue("name")), "phone": strings.TrimSpace(r.FormValue("phone")), "address": strings.TrimSpace(r.FormValue("address"))}
	errs := map[string]string{}
	if l := utf8.RuneCountInString(form["name"]); l < 1 || l > 100 {
		errs["name"] = "Please enter your name."
	}
	if form["phone"] != "" {
		if p, ok := normalizePhone(form["phone"]); ok {
			form["phone"] = p
		} else {
			errs["phone"] = "Enter a valid Bangladeshi mobile number."
		}
	}
	if utf8.RuneCountInString(form["address"]) > 500 {
		errs["address"] = "Address is too long."
	}
	if len(errs) > 0 {
		// Don't swallow a store error here: this page is mid-edit, and a
		// failure must 500 rather than silently render "no orders".
		orders, err := a.st.ListOrdersByUser(r.Context(), u.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		a.renderStatus(w, r, http.StatusUnprocessableEntity, "store/account.html", page{Title: "Your account", NoIndex: true, Data: accountData{User: u, Orders: orders, Form: form, Errors: errs}})
		return
	}
	if err := a.st.UpdateProfile(r.Context(), u.ID, form["name"], form["phone"], form["address"]); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.setFlash(w, "Saved.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}
