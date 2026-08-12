package books

import "strings"

// NormalizeISBN strips separators, uppercases an X check digit, and verifies
// the checksum. Validating locally means a typo costs nothing upstream, and it
// keeps the ISBN written to the database in one canonical shape.
func NormalizeISBN(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'x' || r == 'X':
			b.WriteRune('X')
		case r == '-' || r == ' ':
			// Separator; drop it.
		default:
			return "", ErrInvalidISBN
		}
	}

	s := b.String()
	switch {
	case len(s) == 10 && validISBN10(s):
		return s, nil
	case len(s) == 13 && validISBN13(s):
		return s, nil
	default:
		return "", ErrInvalidISBN
	}
}

// validISBN10 weights the digits 10 down to 1; the total must be divisible by
// 11. Only the final digit may be X, standing for 10.
func validISBN10(s string) bool {
	sum := 0
	for i, r := range s {
		d := 0
		switch {
		case r == 'X':
			if i != 9 {
				return false
			}
			d = 10
		case r >= '0' && r <= '9':
			d = int(r - '0')
		default:
			return false
		}
		sum += d * (10 - i)
	}
	return sum%11 == 0
}

// validISBN13 alternates weights 1 and 3; the total must be divisible by 10.
// X is never a valid ISBN-13 character.
func validISBN13(s string) bool {
	sum := 0
	for i, r := range s {
		if r < '0' || r > '9' {
			return false
		}
		d := int(r - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return sum%10 == 0
}
