package stats

import (
	"context"
	"time"
)

// Store aggregates one user's reading statistics. *store.SQLite implements
// it; the interface keeps this package free of database/sql and lets
// scope/year validation and limit clamping be tested against a fake.
//
// Every method takes userID and scopes by it — a caller only ever sees
// counts and rankings over their own books.
type Store interface {
	// Summary aggregates counts and this/last year totals. now is threaded
	// through rather than read from the system clock so the year boundary
	// is testable without sleeping past a rollover.
	Summary(ctx context.Context, userID int64, now time.Time) (Summary, error)
	// ByYear groups dated read books by year, ascending. The returned int is
	// the count of read books with no finished_at.
	ByYear(ctx context.Context, userID int64) ([]YearCount, int, error)
	// ByMonth returns exactly 12 zero-filled entries for the given year.
	// year arrives already validated as a four-digit string.
	ByMonth(ctx context.Context, userID int64, year string) ([]MonthCount, error)
	// ByLanguage groups read books by language within scope ("all" or a
	// four-digit year, already validated), sorted by count descending then
	// language ascending.
	ByLanguage(ctx context.Context, userID int64, scope string) ([]LanguageCount, error)
	// TopAuthors groups read books by author within scope, sorted by count
	// descending then author ascending, capped at limit.
	TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]AuthorCount, error)
	// Streak counts consecutive calendar months, counting back from the
	// last completed month before now, with at least one read book
	// finished in that month.
	Streak(ctx context.Context, userID int64, now time.Time) (int, error)
}
