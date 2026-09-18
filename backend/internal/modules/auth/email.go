package auth

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

//go:embed templates/*.html
var emailTemplatesFS embed.FS

var emailTemplates = template.Must(template.ParseFS(emailTemplatesFS, "templates/*.html"))

type emailLinkData struct {
	Greeting string
	Link     string
}

type Mailer interface {
	SendVerificationEmail(ctx context.Context, to, username, token string) error
	SendPasswordResetEmail(ctx context.Context, to, username, token string) error
}

type SMTPMailer struct {
	Addr       string
	From       string
	WebBaseURL string
}

func NewSMTPMailer(addr, from, webBaseURL string) *SMTPMailer {
	return &SMTPMailer{Addr: addr, From: from, WebBaseURL: webBaseURL}
}

type emailMessage struct {
	Subject string
	Body    string
}

func (m *SMTPMailer) SendVerificationEmail(ctx context.Context, to, username, token string) error {
	body, err := m.renderTemplate("email_verification.html", emailLinkData{
		Greeting: greetingFor(username, to),
		Link:     m.link("/verify-email", token),
	})
	if err != nil {
		return fmt.Errorf("render verification email: %w", err)
	}
	return m.send(to, emailMessage{Subject: "Confirm your email", Body: body})
}

func (m *SMTPMailer) SendPasswordResetEmail(ctx context.Context, to, username, token string) error {
	body, err := m.renderTemplate("email_password_reset.html", emailLinkData{
		Greeting: greetingFor(username, to),
		Link:     m.link("/reset-password", token),
	})
	if err != nil {
		return fmt.Errorf("render password reset email: %w", err)
	}
	return m.send(to, emailMessage{Subject: "Reset your password", Body: body})
}

func (m *SMTPMailer) renderTemplate(name string, data emailLinkData) (string, error) {
	var buf bytes.Buffer
	if err := emailTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *SMTPMailer) link(path, token string) string {
	return strings.TrimRight(m.WebBaseURL, "/") + path + "?token=" + url.QueryEscape(token)
}

func (m *SMTPMailer) send(to string, msg emailMessage) error {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/html; charset=utf-8\r\n")
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "\r\n")
	b.WriteString(msg.Body)

	return smtp.SendMail(m.Addr, nil, m.From, []string{to}, []byte(b.String()))
}

func greetingFor(username, to string) string {
	if username != "" {
		return username
	}
	return to
}
