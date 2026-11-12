package email

import (
	"strings"
	"testing"
)

func TestSanitizeHeader(t *testing.T) {
	if got := sanitizeHeader("hello\r\nBcc: evil@example.com"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("header injection survived: %q", got)
	}
	if got := sanitizeHeader("  hi  "); got != "hi" {
		t.Fatalf("trim = %q", got)
	}
}

func TestBuildMessage(t *testing.T) {
	raw := string(buildMessage("Clavis <no-reply@example.com>", "user@example.com", "Reset\r\nBcc: x", "line1\nline2"))
	if strings.Contains(raw, "\r\nBcc: x") {
		t.Fatalf("subject injection survived: %q", raw)
	}
	if !strings.Contains(raw, "Content-Type: text/plain") || !strings.Contains(raw, "line1\r\nline2") {
		t.Fatalf("message = %q", raw)
	}
}

func TestTemplates(t *testing.T) {
	reset := ResetMessage("user@example.com", "https://clavis.example.com/reset?token=abc")
	if reset.To != "user@example.com" || !strings.Contains(reset.Text, "reset?token=abc") || !strings.Contains(reset.Subject, "Reset") {
		t.Fatalf("reset = %+v", reset)
	}
	invite := InviteMessage("user@example.com", "Acme", "Developer", "https://clavis.example.com/join?token=abc")
	if !strings.Contains(invite.Text, "Acme") || !strings.Contains(invite.Text, "Developer") || !strings.Contains(invite.Text, "join?token=abc") {
		t.Fatalf("invite = %+v", invite)
	}
	verify := VerifyMessage("user@example.com", "https://clavis.example.com/verify?token=abc")
	if !strings.Contains(verify.Subject, "Verify") || !strings.Contains(verify.Text, "verify?token=abc") {
		t.Fatalf("verify = %+v", verify)
	}
}

func TestSendRejectsInvalidAddresses(t *testing.T) {
	sender := &SMTPSender{Host: "127.0.0.1", Port: 1, From: "Clavis <no-reply@example.com>", TLS: "none"}
	if err := sender.Send(t.Context(), Message{To: "not-an-email", Subject: "x"}); err == nil {
		t.Fatal("invalid recipient accepted")
	}
	sender.From = "not-an-email"
	if err := sender.Send(t.Context(), Message{To: "user@example.com", Subject: "x"}); err == nil {
		t.Fatal("invalid from accepted")
	}
	sender.From = "Clavis <no-reply@example.com>"
	if err := sender.Send(t.Context(), Message{To: "user@example.com", Subject: " "}); err == nil {
		t.Fatal("empty subject accepted")
	}
}
