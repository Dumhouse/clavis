// Package email sends transactional mail (password resets and invites).
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is one plain-text email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender sends mail. It is nil in the server when SMTP is not configured.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPSender talks to an SMTP server using implicit TLS, STARTTLS, or plain
// text (for local test servers only).
type SMTPSender struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      string // starttls | tls | none
	Timeout  time.Duration
}

// Send implements Sender.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if s.Timeout <= 0 {
		s.Timeout = 15 * time.Second
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("email: invalid recipient: %w", err)
	}
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("email: invalid from address: %w", err)
	}
	subject := sanitizeHeader(msg.Subject)
	if subject == "" {
		return fmt.Errorf("email: subject is required")
	}

	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	addr := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: connect: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	if s.TLS == "tls" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("email: tls handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer client.Close()

	if s.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("email: server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}
	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := writer.Write(buildMessage(s.From, to.Address, subject, msg.Text)); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("email: finish body: %w", err)
	}
	return client.Quit()
}

func buildMessage(from, to, subject, text string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", sanitizeHeader(from))
	fmt.Fprintf(&b, "To: %s\r\n", sanitizeHeader(to))
	fmt.Fprintf(&b, "Subject: %s\r\n", sanitizeHeader(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// sanitizeHeader prevents header injection via user-controlled values.
func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

// Templates returns ready-to-send transactional messages.
func ResetMessage(to, resetURL string) Message {
	return Message{
		To:      to,
		Subject: "Reset your Clavis password",
		Text: "Someone requested a password reset for your Clavis account.\n\n" +
			"Open this link within 30 minutes:\n" + resetURL + "\n\n" +
			"If you did not request this, you can ignore this message. Your password will not change.\n",
	}
}

// InviteMessage returns an invite email.
func InviteMessage(to, orgName, role, inviteURL string) Message {
	return Message{
		To:      to,
		Subject: "You have been invited to " + orgName + " on Clavis",
		Text: "You have been invited to join " + orgName + " on Clavis as " + role + ".\n\n" +
			"Open this single-use link to accept:\n" + inviteURL + "\n\n" +
			"If you were not expecting this, you can ignore the message.\n",
	}
}

// VerifyMessage returns an email confirmation message.
func VerifyMessage(to, verifyURL string) Message {
	return Message{
		To:      to,
		Subject: "Verify your Clavis email address",
		Text: "Confirm this address for your Clavis account.\n\n" +
			"Open this link within 24 hours:\n" + verifyURL + "\n\n" +
			"If you did not create an account, you can ignore this message.\n",
	}
}
