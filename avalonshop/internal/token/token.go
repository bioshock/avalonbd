// Package token signs small values so they can live in cookies and URLs.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxCartLines = 20

type Signer struct{ key []byte }

func New(key []byte) Signer { return Signer{key: key} }

func (s Signer) sign(msg string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

func (s Signer) verify(msg, sig string) bool {
	return hmac.Equal([]byte(s.sign(msg)), []byte(sig))
}

type CartLine struct {
	VariantID int64 `json:"v"`
	Qty       int   `json:"q"`
}

func (s Signer) EncodeCart(lines []CartLine) string {
	b, _ := json.Marshal(lines)
	enc := base64.RawURLEncoding.EncodeToString(b)
	return enc + "." + s.sign(enc)
}

// DecodeCart returns nil for a missing, malformed, or tampered value. Invalid
// lines are dropped and the result is capped at MaxCartLines.
func (s Signer) DecodeCart(v string) []CartLine {
	enc, sig, ok := strings.Cut(v, ".")
	if !ok || !s.verify(enc, sig) {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil
	}
	var lines []CartLine
	if json.Unmarshal(b, &lines) != nil {
		return nil
	}
	out := lines[:0]
	for _, l := range lines {
		if l.VariantID > 0 && l.Qty >= 1 && l.Qty <= 99 {
			out = append(out, l)
		}
	}
	if len(out) > MaxCartLines {
		out = out[:MaxCartLines]
	}
	return out
}

func (s Signer) EncodeSession(userID int64, exp time.Time) string {
	msg := fmt.Sprintf("%d.%d", userID, exp.Unix())
	return msg + "." + s.sign(msg)
}

func (s Signer) DecodeSession(v string, now time.Time) (int64, bool) {
	p := strings.Split(v, ".")
	if len(p) != 3 || !s.verify(p[0]+"."+p[1], p[2]) {
		return 0, false
	}
	id, err1 := strconv.ParseInt(p[0], 10, 64)
	exp, err2 := strconv.ParseInt(p[1], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 || now.Unix() >= exp {
		return 0, false
	}
	return id, true
}

// ResetToken binds the token to the current password hash, so it stops
// working once the password changes. That makes it single-use without a table.
func (s Signer) ResetToken(userID int64, exp time.Time, passwordHash string) string {
	msg := fmt.Sprintf("%d.%d", userID, exp.Unix())
	return msg + "." + s.sign(msg+"."+passwordHash)
}

func (s Signer) ParseReset(tok string, now time.Time, lookupHash func(int64) (string, bool)) (int64, bool) {
	p := strings.Split(tok, ".")
	if len(p) != 3 {
		return 0, false
	}
	id, err1 := strconv.ParseInt(p[0], 10, 64)
	exp, err2 := strconv.ParseInt(p[1], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 || now.Unix() >= exp {
		return 0, false
	}
	hash, ok := lookupHash(id)
	if !ok || !s.verify(p[0]+"."+p[1]+"."+hash, p[2]) {
		return 0, false
	}
	return id, true
}

func (s Signer) OrderToken(number string) string { return s.sign("order:" + number)[:16] }

func (s Signer) VerifyOrderToken(number, t string) bool {
	return hmac.Equal([]byte(s.OrderToken(number)), []byte(t))
}
