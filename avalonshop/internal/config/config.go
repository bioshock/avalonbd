// Package config reads settings from the environment.
package config

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"
)

type Config struct {
	Addr             string
	DatabaseURL      string
	BaseURL          string
	SessionSecret    []byte
	UploadDir        string
	SMTPHost         string
	SMTPPort         string
	SMTPUser         string
	SMTPPass         string
	MailFrom         string
	OrderNotifyEmail string
	AdminEmail       string
	AdminPassword    string
}

// Secure reports whether cookies should carry the Secure flag.
func (c Config) Secure() bool { return strings.HasPrefix(c.BaseURL, "https://") }

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	def := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	c := Config{
		Addr:             def(get("ADDR"), ":8080"),
		DatabaseURL:      get("DATABASE_URL"),
		BaseURL:          strings.TrimRight(get("BASE_URL"), "/"),
		UploadDir:        def(get("UPLOAD_DIR"), "./data/uploads"),
		SMTPHost:         get("SMTP_HOST"),
		SMTPPort:         def(get("SMTP_PORT"), "587"),
		SMTPUser:         get("SMTP_USER"),
		SMTPPass:         get("SMTP_PASS"),
		MailFrom:         get("MAIL_FROM"),
		OrderNotifyEmail: get("ORDER_NOTIFY_EMAIL"),
		AdminEmail:       get("ADMIN_EMAIL"),
		AdminPassword:    get("ADMIN_PASSWORD"),
	}
	for _, kv := range [][2]string{
		{"DATABASE_URL", c.DatabaseURL}, {"BASE_URL", c.BaseURL},
		{"MAIL_FROM", c.MailFrom}, {"ORDER_NOTIFY_EMAIL", c.OrderNotifyEmail},
	} {
		if kv[1] == "" {
			return c, fmt.Errorf("config: %s is required", kv[0])
		}
	}
	sec, err := hex.DecodeString(get("SESSION_SECRET"))
	if err != nil || len(sec) < 32 {
		return c, errors.New("config: SESSION_SECRET must be at least 64 hex characters")
	}
	c.SessionSecret = sec

	// MAIL_FROM is used as both the SMTP envelope sender and the From:
	// header (internal/mail). A display-name form such as
	// "Avalon Corporation <shop@avalonbd.com>" — the exact string Brevo's
	// Senders screen shows, and the natural thing to paste — produces an
	// invalid MAIL FROM:<...> on the wire that a real relay rejects, and
	// because Send only logs failures, every order confirmation, tracking
	// link, admin alert and password reset then fails silently. Reject it
	// at boot instead (C4).
	if addr, err := mail.ParseAddress(c.MailFrom); err != nil || addr.Address != c.MailFrom {
		return c, fmt.Errorf("config: MAIL_FROM must be a bare email address with no display name (e.g. shop@avalonbd.com), got %q", c.MailFrom)
	}
	return c, nil
}

// LoadDotEnv sets KEY=VALUE lines from path into the environment unless already set.
// A missing file is not an error. ponytail: 20-line parser instead of a dotenv module.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}
