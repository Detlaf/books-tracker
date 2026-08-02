package auth

import "testing"

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"a@b.com", "a@b.com"},
		{"A@B.com", "a@b.com"},
		{"  a@b.com  ", "a@b.com"},
		{"\tMixed@Case.COM\n", "mixed@case.com"},
	}
	for _, tt := range tests {
		if got := NormalizeEmail(tt.in); got != tt.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
