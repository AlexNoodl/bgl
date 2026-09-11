package auth

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

const (
	minPasswordLength = 8
	maxPasswordLength = 128

	minUsernameLength = 3
	maxUsernameLength = 32
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func normalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("email is required")
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", fmt.Errorf("email is not a valid email address")
	}
	if addr.Address != trimmed {
		return "", fmt.Errorf("email is not a valid email address")
	}
	return trimmed, nil
}

func validateUsername(username string) error {
	if len(username) < minUsernameLength || len(username) > maxUsernameLength {
		return fmt.Errorf("username must be between %d and %d characters", minUsernameLength, maxUsernameLength)
	}
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("username may only contain letters, numbers, and underscores")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return fmt.Errorf("password must be at most %d characters", maxPasswordLength)
	}
	return nil
}
