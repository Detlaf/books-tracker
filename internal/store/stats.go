package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/kate/book-tracking/internal/stats"
)

func (s *SQLite) Summary(ctx context.Context, userID int64, now time.Time) (stats.Summary, error) {
	year := now.UTC().Year()

	const q = `SELECT
		COALESCE(SUM(CASE WHEN status = 'read' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'reading' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'backlog' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'read' AND finished_at IS NOT NULL
			AND strftime('%Y', finished_at) = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'read' AND finished_at IS NOT NULL
			AND strftime('%Y', finished_at) = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'read' AND finished_at IS NULL THEN 1 ELSE 0 END), 0)
		FROM user_books WHERE user_id = ?`

	row := s.db.QueryRowContext(ctx, q, strconv.Itoa(year), strconv.Itoa(year-1), userID)
	var sm stats.Summary
	if err := row.Scan(&sm.TotalRead, &sm.Reading, &sm.Backlog, &sm.ThisYear, &sm.LastYear, &sm.UndatedRead); err != nil {
		return stats.Summary{}, fmt.Errorf("stats summary: %w", err)
	}
	sm.CurrentYear = year
	return sm, nil
}

func (s *SQLite) ByYear(ctx context.Context, userID int64) ([]stats.YearCount, int, error) {
	const q = `SELECT strftime('%Y', finished_at) AS year, COUNT(*)
		FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NOT NULL
		GROUP BY year ORDER BY year ASC`

	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("stats by year: %w", err)
	}
	defer rows.Close()

	var years []stats.YearCount
	for rows.Next() {
		var y stats.YearCount
		if err := rows.Scan(&y.Year, &y.Count); err != nil {
			return nil, 0, fmt.Errorf("stats by year: %w", err)
		}
		years = append(years, y)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("stats by year: %w", err)
	}

	const undatedQ = `SELECT COUNT(*) FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NULL`
	var undated int
	if err := s.db.QueryRowContext(ctx, undatedQ, userID).Scan(&undated); err != nil {
		return nil, 0, fmt.Errorf("stats by year undated: %w", err)
	}
	return years, undated, nil
}

func (s *SQLite) ByMonth(ctx context.Context, userID int64, year string) ([]stats.MonthCount, error) {
	const q = `SELECT CAST(strftime('%m', finished_at) AS INTEGER), COUNT(*)
		FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NOT NULL
		  AND strftime('%Y', finished_at) = ?
		GROUP BY 1`

	rows, err := s.db.QueryContext(ctx, q, userID, year)
	if err != nil {
		return nil, fmt.Errorf("stats by month: %w", err)
	}
	defer rows.Close()

	counts := map[int]int{}
	for rows.Next() {
		var month, count int
		if err := rows.Scan(&month, &count); err != nil {
			return nil, fmt.Errorf("stats by month: %w", err)
		}
		counts[month] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats by month: %w", err)
	}

	months := make([]stats.MonthCount, 12)
	for i := 0; i < 12; i++ {
		months[i] = stats.MonthCount{Month: i + 1, Count: counts[i+1]}
	}
	return months, nil
}

func (s *SQLite) ByLanguage(ctx context.Context, userID int64, scope string) ([]stats.LanguageCount, error) {
	args := []any{userID}
	where := `ub.user_id = ? AND ub.status = 'read'`
	if scope != "all" {
		where += ` AND ub.finished_at IS NOT NULL AND strftime('%Y', ub.finished_at) = ?`
		args = append(args, scope)
	}

	q := `SELECT COALESCE(NULLIF(b.language, ''), 'Unknown') AS language, COUNT(*)
		FROM user_books ub
		JOIN books b ON b.id = ub.book_id
		WHERE ` + where + `
		GROUP BY 1
		ORDER BY COUNT(*) DESC, language ASC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stats by language: %w", err)
	}
	defer rows.Close()

	var languages []stats.LanguageCount
	for rows.Next() {
		var l stats.LanguageCount
		if err := rows.Scan(&l.Language, &l.Count); err != nil {
			return nil, fmt.Errorf("stats by language: %w", err)
		}
		languages = append(languages, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats by language: %w", err)
	}
	return languages, nil
}

func (s *SQLite) TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]stats.AuthorCount, error) {
	args := []any{userID}
	where := `ub.user_id = ? AND ub.status = 'read'`
	if scope != "all" {
		where += ` AND ub.finished_at IS NOT NULL AND strftime('%Y', ub.finished_at) = ?`
		args = append(args, scope)
	}
	args = append(args, limit)

	q := `SELECT ba.name, COUNT(DISTINCT ub.book_id)
		FROM user_books ub
		JOIN book_authors ba ON ba.book_id = ub.book_id
		WHERE ` + where + `
		GROUP BY ba.name
		ORDER BY COUNT(*) DESC, ba.name ASC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stats top authors: %w", err)
	}
	defer rows.Close()

	var authors []stats.AuthorCount
	for rows.Next() {
		var a stats.AuthorCount
		if err := rows.Scan(&a.Author, &a.Count); err != nil {
			return nil, fmt.Errorf("stats top authors: %w", err)
		}
		authors = append(authors, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats top authors: %w", err)
	}
	return authors, nil
}

// Streak loads every distinct year-month with a finished read book in one
// query, then walks backward in Go — the same 240-month-bounded loop
// reports.js.currentStreak uses — rather than issuing up to 240 queries.
func (s *SQLite) Streak(ctx context.Context, userID int64, now time.Time) (int, error) {
	const q = `SELECT DISTINCT strftime('%Y-%m', finished_at)
		FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NOT NULL`

	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return 0, fmt.Errorf("stats streak: %w", err)
	}
	defer rows.Close()

	finished := map[string]bool{}
	for rows.Next() {
		var ym string
		if err := rows.Scan(&ym); err != nil {
			return 0, fmt.Errorf("stats streak: %w", err)
		}
		finished[ym] = true
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("stats streak: %w", err)
	}

	y, m := now.UTC().Year(), int(now.UTC().Month())
	streak := 0
	for i := 0; i < 240; i++ {
		m--
		if m == 0 {
			m = 12
			y--
		}
		if finished[fmt.Sprintf("%04d-%02d", y, m)] {
			streak++
		} else {
			break
		}
	}
	return streak, nil
}

// compile-time guard: the SQL layer must keep satisfying what the service
// depends on.
var _ stats.Store = (*SQLite)(nil)
