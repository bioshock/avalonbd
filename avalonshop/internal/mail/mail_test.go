package mail

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestRenderAndDevSend(t *testing.T) {
	var logbuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logbuf, nil))
	m, err := New("", "587", "", "", "shop@example.com", os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}
	subj, body, err := m.Render("password_reset", map[string]any{"Name": "Ana", "ResetURL": "https://x/reset/abc"})
	if err != nil {
		t.Fatal(err)
	}
	if subj != "Reset your Avalon Foods password" || !strings.Contains(body, "https://x/reset/abc") || strings.HasPrefix(body, "Subject:") {
		t.Fatalf("subject=%q body=%q", subj, body)
	}
	if err := m.SendNow("ana@example.com", "password_reset", map[string]any{"Name": "Ana", "ResetURL": "u"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logbuf.String(), "ana@example.com") {
		t.Fatalf("dev mode should log the send: %s", logbuf.String())
	}
	if _, _, err := m.Render("nope", nil); err == nil {
		t.Fatal("unknown template should error")
	}
	logbuf.Reset()
	m.Send("ana@example.com", "password_reset", map[string]any{"Name": "Ana", "ResetURL": "u"})
	m.Wait()
	if !strings.Contains(logbuf.String(), "ana@example.com") {
		t.Fatalf("async send should log the send: %s", logbuf.String())
	}
}

func TestBuildMessage(t *testing.T) {
	msg := buildMessage("shop@example.com", "ana@example.com", "Order ৳ 650", "line one\nটাকা\n")
	s := string(msg)
	for _, want := range []string{"From: shop@example.com\r\n", "To: ana@example.com\r\n", "Subject: =?utf-8?q?", "Content-Type: text/plain; charset=utf-8\r\n", "Content-Transfer-Encoding: quoted-printable\r\n", "\r\n\r\nline one\r\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q:\n%s", want, s)
		}
	}
}

func TestRejectAddressWithCRLF(t *testing.T) {
	var logbuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logbuf, nil))
	m, err := New("", "587", "", "", "shop@example.com", os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}

	// Test recipient with CR/LF injection attempt
	err = m.SendNow("ana@example.com\r\nBcc: attacker@example.com", "password_reset", map[string]any{"Name": "Ana", "ResetURL": "u"})
	if err == nil {
		t.Fatal("SendNow should reject recipient with CR/LF")
	}
	if strings.Contains(logbuf.String(), "ana@example.com\r\nBcc") {
		t.Fatal("should not log successful send for rejected address")
	}

	// Test sender with CR/LF injection attempt (if reachable through New)
	m2, err := New("", "587", "", "", "shop@example.com\r\nCc: attacker@example.com", os.DirFS("../.."), log)
	if err == nil {
		logbuf.Reset()
		err = m2.SendNow("ana@example.com", "password_reset", map[string]any{"Name": "Ana", "ResetURL": "u"})
		if err == nil {
			t.Fatal("SendNow should reject sender with CR/LF")
		}
	}
}

func TestSendAfterWait(t *testing.T) {
	var logbuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logbuf, nil))
	m, err := New("", "587", "", "", "shop@example.com", os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}

	// First normal send and wait
	m.Send("user1@example.com", "password_reset", map[string]any{"Name": "User1", "ResetURL": "u"})
	m.Wait()

	// Now send after wait - should not panic and should log drop
	logbuf.Reset()
	m.Send("user2@example.com", "password_reset", map[string]any{"Name": "User2", "ResetURL": "u"})
	m.Wait() // Wait again - should be safe

	if !strings.Contains(logbuf.String(), "user2@example.com") {
		t.Fatalf("should log dropped send: %s", logbuf.String())
	}
	if !strings.Contains(logbuf.String(), "after shutdown") {
		t.Fatalf("should indicate shutdown in log: %s", logbuf.String())
	}
}
