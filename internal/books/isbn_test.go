package books

import (
	"errors"
	"testing"
)

func TestNormalizeISBNAccepts(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"isbn-13", "9780441013593", "9780441013593"},
		{"isbn-13 hyphenated", "978-0-441-01359-3", "9780441013593"},
		{"isbn-13 spaced", "978 0 441 01359 3", "9780441013593"},
		{"isbn-10", "0441013597", "0441013597"},
		{"isbn-10 with X check digit", "080442957X", "080442957X"},
		{"isbn-10 with lowercase x", "080442957x", "080442957X"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeISBN(tc.in)
			if err != nil {
				t.Fatalf("NormalizeISBN(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeISBN(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeISBNRejects(t *testing.T) {
	cases := []struct{ name, in string }{
		{"empty", ""},
		{"too short", "12345"},
		{"too long", "97804410135931"},
		{"bad isbn-13 checksum", "9780441013594"},
		{"bad isbn-10 checksum", "0441013598"},
		{"X in the wrong place", "04410X3597"},
		{"letters", "notanisbn0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeISBN(tc.in); !errors.Is(err, ErrInvalidISBN) {
				t.Fatalf("NormalizeISBN(%q) error = %v, want ErrInvalidISBN", tc.in, err)
			}
		})
	}
}
