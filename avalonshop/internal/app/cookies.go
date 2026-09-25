package app

import (
	"net/http"
	"net/url"
	"time"

	"avalonshop/internal/token"
)

const sessionTTL = 30 * 24 * time.Hour

func (a *App) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.cfg.Secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) cartLines(r *http.Request) []token.CartLine {
	c, err := r.Cookie("cart")
	if err != nil {
		return nil
	}
	return a.tok.DecodeCart(c.Value)
}

func (a *App) saveCart(w http.ResponseWriter, lines []token.CartLine) {
	if len(lines) == 0 {
		a.setCookie(w, "cart", "", -1)
		return
	}
	a.setCookie(w, "cart", a.tok.EncodeCart(lines), int(sessionTTL.Seconds()))
}

func cartCount(lines []token.CartLine) int {
	n := 0
	for _, l := range lines {
		n += l.Qty
	}
	return n
}

func (a *App) login(w http.ResponseWriter, userID int64) {
	a.setCookie(w, "sess", a.tok.EncodeSession(userID, time.Now().Add(sessionTTL)), int(sessionTTL.Seconds()))
}

func (a *App) logout(w http.ResponseWriter) { a.setCookie(w, "sess", "", -1) }

// Flash is a one-shot message. It is URL-escaped because cookie values cannot hold spaces.
func (a *App) setFlash(w http.ResponseWriter, msg string) {
	a.setCookie(w, "flash", url.QueryEscape(msg), 60)
}

func (a *App) popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("flash")
	if err != nil {
		return ""
	}
	a.setCookie(w, "flash", "", -1)
	msg, _ := url.QueryUnescape(c.Value)
	return msg
}
