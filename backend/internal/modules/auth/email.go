package auth

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

type Mailer interface {
	SendVerificationEmail(ctx context.Context, to, username, token string) error
}

type SMTPMailer struct {
	Addr       string
	From       string
	WebBaseURL string
}

func NewSMTPMailer(addr, from, webBaseURL string) *SMTPMailer {
	return &SMTPMailer{Addr: addr, From: from, WebBaseURL: webBaseURL}
}

func (m *SMTPMailer) SendVerificationEmail(ctx context.Context, to, username, token string) error {
	greeting := username
	if greeting == "" {
		greeting = to
	}
	link := fmt.Sprintf("%s/verify-email?token=%s", m.WebBaseURL, token)

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: Confirm your BGL account\r\n")
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "Hi %s,\r\n\r\n", greeting)
	fmt.Fprintf(&b, "Confirm your email by opening this link:\r\n%s\r\n\r\n", link)
	fmt.Fprintf(&b, "If you did not create this account, you can ignore this email.\r\n")

	return smtp.SendMail(m.Addr, nil, m.From, []string{to}, []byte(b.String()))
}
