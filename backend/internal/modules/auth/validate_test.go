package auth

import "testing"

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		want    string
	}{
		{"user@example.com", false, "user@example.com"},
		{"  user@example.com  ", false, "user@example.com"},
		{"not-an-email", true, ""},
		{"", true, ""},
		{"Display Name <user@example.com>", true, ""},
	}
	for _, c := range cases {
		got, err := normalizeEmail(c.in)
		if c.wantErr && err == nil {
			t.Errorf("normalizeEmail(%q): expected error, got nil (result %q)", c.in, got)
		}
		if !c.wantErr && err != nil {
			t.Errorf("normalizeEmail(%q): unexpected error: %v", c.in, err)
		}
		if !c.wantErr && got != c.want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"ab", true}, // too short
		{"abc", false},
		{"schema_test_user", false},
		{"has space", true},
		{"has-dash", true},
		{"has.dot", true},
		{"this_username_is_way_too_long_to_be_valid_ok", true}, // too long
	}
	for _, c := range cases {
		err := validateUsername(c.in)
		if c.wantErr && err == nil {
			t.Errorf("validateUsername(%q): expected error, got nil", c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateUsername(%q): unexpected error: %v", c.in, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"short1", true},
		{"12345678", false},
		{"", true},
	}
	for _, c := range cases {
		err := validatePassword(c.in)
		if c.wantErr && err == nil {
			t.Errorf("validatePassword(%q): expected error, got nil", c.in)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validatePassword(%q): unexpected error: %v", c.in, err)
		}
	}
}
