package stats

import "errors"

var (
	// ErrInvalidScope means scope was neither "all" nor a four-digit year.
	ErrInvalidScope = errors.New(`scope must be "all" or a four-digit year`)
	// ErrInvalidYear means year was not a four-digit year.
	ErrInvalidYear = errors.New("year must be a four-digit year")
)
