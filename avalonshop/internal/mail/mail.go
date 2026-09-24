// Package mail renders plain-text templates and sends them over SMTP.
package mail

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net/smtp"
	"strings"
	"sync"
	"text/template"
	"time"

	"avalonshop/internal/money"
)

type Mailer struct {
	host, port, user, pass, from string
	tmpl                         *template.Template
	log                          *slog.Logger
	wg                           sync.WaitGroup
	mu                           sync.Mutex
	closed                       bool
}

// New parses templates/email/*.txt. With an empty host, Send logs instead of sending.
func New(host, port, user, pass, from string, fsys fs.FS, log *slog.Logger) (*Mailer, error) {
	t, err := template.New("email").Funcs(template.FuncMap{"taka": money.Format}).ParseFS(fsys, "templates/email/*.txt")
	if err != nil {
		return nil, err
	}
	return &Mailer{host: host, port: port, user: user, pass: pass, from: from, tmpl: t, log: log}, nil
}

// Render executes the named template. The first line must be "Subject: ...".
func (m *Mailer) Render(name string, data any) (subject, body string, err error) {
	var buf bytes.Buffer
	if err := m.tmpl.ExecuteTemplate(&buf, name+".txt", data); err != nil {
		return "", "", err
	}
	first, rest, _ := strings.Cut(buf.String(), "\n")
	if !strings.HasPrefix(first, "Subject: ") {
		return "", "", fmt.Errorf("mail: template %s must start with a Subject line", name)
	}
	return strings.TrimPrefix(first, "Subject: "), strings.TrimLeft(rest, "\n"), nil
}

// Send delivers in the background. Failures are logged, never returned:
// an order must succeed even if email is down.
func (m *Mailer) Send(to, name string, data any) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.log.Warn("mail dropped after shutdown", "to", to, "template", name)
		return
	}
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		subject, err := m.sendAndGetSubject(to, name, data)
		if err != nil {
			m.log.Error("mail failed", "to", to, "template", name, "subject", subject, "err", err)
		}
	}()
}

// Wait stops accepting new sends and waits for in-flight sends to complete.
// Calling Wait multiple times is safe.
func (m *Mailer) Wait() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Mailer) SendNow(to, name string, data any) error {
	_, err := m.sendAndGetSubject(to, name, data)
	return err
}

// sendAndGetSubject sends the email and returns the rendered subject.
// If rendering fails, subject will be empty.
func (m *Mailer) sendAndGetSubject(to, name string, data any) (string, error) {
	subject, body, err := m.Render(name, data)
	if err != nil {
		return "", err
	}
	if err := validAddr(to); err != nil {
		return subject, err
	}
	if err := validAddr(m.from); err != nil {
		return subject, err
	}
	if m.host == "" {
		m.log.Info("mail (dev mode, not sent)", "to", to, "subject", subject, "body", body)
		return subject, nil
	}
	msg := buildMessage(m.from, to, subject, body)
	auth := smtp.PlainAuth("", m.user, m.pass, m.host)
	if err := smtp.SendMail(m.host+":"+m.port, auth, m.from, []string{to}, msg); err != nil {
		return subject, err
	}
	return subject, nil
}

// validAddr rejects addresses containing CR or LF to prevent header injection.
func validAddr(addr string) error {
	if strings.ContainsAny(addr, "\r\n") {
		return fmt.Errorf("mail: address contains CR or LF")
	}
	return nil
}

func buildMessage(from, to, subject, body string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n",
		from, to, mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z))
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
	qp.Close()
	return b.Bytes()
}
