package store

import (
	"context"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/library"
)

// seedRead adds a book to userID's library as read with the given
// finished_at (nil for undated).
func seedRead(t *testing.T, s *SQLite, userID, bookID int64, finishedAt *time.Time) {
	t.Helper()
	if _, err := s.AddEntry(context.Background(), userID, bookID, library.StatusRead, finishedAt); err != nil {
		t.Fatalf("seedRead: %v", err)
	}
}

func datedAt(rfc3339 string) *time.Time {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestSummaryCountsByStatusAndYear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)

	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	b2 := seedBook(t, s, "vol-2", "Book 2", nil)
	b3 := seedBook(t, s, "vol-3", "Book 3", nil)
	b4 := seedBook(t, s, "vol-4", "Book 4", nil)
	b5 := seedBook(t, s, "vol-5", "Book 5", nil)

	seedRead(t, s, userID, b1, datedAt("2026-03-01T00:00:00Z")) // this year
	seedRead(t, s, userID, b2, datedAt("2025-03-01T00:00:00Z")) // last year
	seedRead(t, s, userID, b3, nil)                             // undated
	if _, err := s.AddEntry(ctx, userID, b4, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, b5, library.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.Summary(ctx, userID, now)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.TotalRead != 3 {
		t.Errorf("TotalRead = %d, want 3", got.TotalRead)
	}
	if got.Reading != 1 || got.Backlog != 1 {
		t.Errorf("Reading = %d, Backlog = %d, want 1, 1", got.Reading, got.Backlog)
	}
	if got.ThisYear != 1 || got.LastYear != 1 {
		t.Errorf("ThisYear = %d, LastYear = %d, want 1, 1", got.ThisYear, got.LastYear)
	}
	if got.CurrentYear != 2026 {
		t.Errorf("CurrentYear = %d, want 2026", got.CurrentYear)
	}
	if got.UndatedRead != 1 {
		t.Errorf("UndatedRead = %d, want 1", got.UndatedRead)
	}
}

func TestSummaryZeroBooksIsAllZero(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	got, err := s.Summary(context.Background(), userID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.TotalRead != 0 || got.Reading != 0 || got.Backlog != 0 {
		t.Fatalf("got = %+v, want all zero", got)
	}
}

func TestByYearExcludesUndatedAndReportsThemSeparately(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	b2 := seedBook(t, s, "vol-2", "Book 2", nil)
	b3 := seedBook(t, s, "vol-3", "Book 3", nil)

	seedRead(t, s, userID, b1, datedAt("2025-01-01T00:00:00Z"))
	seedRead(t, s, userID, b2, datedAt("2026-06-01T00:00:00Z"))
	seedRead(t, s, userID, b3, nil)

	years, undated, err := s.ByYear(ctx, userID)
	if err != nil {
		t.Fatalf("ByYear: %v", err)
	}
	if len(years) != 2 || years[0].Year != "2025" || years[1].Year != "2026" {
		t.Fatalf("years = %+v, want [2025:1 2026:1] ascending", years)
	}
	if undated != 1 {
		t.Fatalf("undated = %d, want 1", undated)
	}
}

func TestByYearOnlyIncludesYearsWithData(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	years, undated, err := s.ByYear(context.Background(), userID)
	if err != nil {
		t.Fatalf("ByYear: %v", err)
	}
	if len(years) != 0 || undated != 0 {
		t.Fatalf("years = %+v, undated = %d, want empty", years, undated)
	}
}

func TestByMonthIsZeroFilledAcrossTwelveMonths(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	seedRead(t, s, userID, b1, datedAt("2026-03-15T00:00:00Z"))

	months, err := s.ByMonth(ctx, userID, "2026")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if len(months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(months))
	}
	for i, m := range months {
		if m.Month != i+1 {
			t.Fatalf("months[%d].Month = %d, want %d", i, m.Month, i+1)
		}
	}
	if months[2].Count != 1 {
		t.Fatalf("March count = %d, want 1", months[2].Count)
	}
	if months[0].Count != 0 {
		t.Fatalf("January count = %d, want 0", months[0].Count)
	}
}

func TestByMonthScopesToTheRequestedYear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	seedRead(t, s, userID, b1, datedAt("2025-03-15T00:00:00Z"))

	months, err := s.ByMonth(ctx, userID, "2026")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	for _, m := range months {
		if m.Count != 0 {
			t.Fatalf("month %d count = %d, want 0 (book was finished in 2025)", m.Month, m.Count)
		}
	}
}

func TestByLanguageMapsMissingLanguageToUnknown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	en1 := seedBook(t, s, "vol-en1", "English 1", nil)
	en2 := seedBook(t, s, "vol-en2", "English 2", nil)
	unset := seedBook(t, s, "vol-unset", "No Language", nil)
	setBookLanguage(t, s, en1, "English")
	setBookLanguage(t, s, en2, "English")

	seedRead(t, s, userID, en1, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, userID, en2, datedAt("2026-02-01T00:00:00Z"))
	seedRead(t, s, userID, unset, nil) // undated but still counted within scope=all

	languages, err := s.ByLanguage(ctx, userID, "all")
	if err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if len(languages) != 2 || languages[0].Language != "English" || languages[0].Count != 2 {
		t.Fatalf("languages = %+v, want English:2 first", languages)
	}
	if languages[1].Language != "Unknown" || languages[1].Count != 1 {
		t.Fatalf("languages[1] = %+v, want Unknown:1", languages[1])
	}
}

func TestByLanguageScopedToYearExcludesUndated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	dated := seedBook(t, s, "vol-dated", "Dated", nil)
	undated := seedBook(t, s, "vol-undated", "Undated", nil)
	setBookLanguage(t, s, dated, "English")
	setBookLanguage(t, s, undated, "English")

	seedRead(t, s, userID, dated, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, userID, undated, nil)

	languages, err := s.ByLanguage(ctx, userID, "2026")
	if err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if len(languages) != 1 || languages[0].Count != 1 {
		t.Fatalf("languages = %+v, want exactly one English:1 (undated excluded by year scope)", languages)
	}
}

func TestTopAuthorsCreditsEveryCoAuthorOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	dune := seedBook(t, s, "vol-dune", "Dune", []string{"Frank Herbert"})
	coauthored := seedBook(t, s, "vol-co", "Co-written", []string{"Ann Leckie", "Frank Herbert"})

	seedRead(t, s, userID, dune, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, userID, coauthored, datedAt("2026-02-01T00:00:00Z"))

	authors, err := s.TopAuthors(ctx, userID, "all", 5)
	if err != nil {
		t.Fatalf("TopAuthors: %v", err)
	}
	if len(authors) != 2 {
		t.Fatalf("authors = %+v, want 2 distinct authors", authors)
	}
	if authors[0].Author != "Frank Herbert" || authors[0].Count != 2 {
		t.Fatalf("authors[0] = %+v, want Frank Herbert:2 (co-authored book counts once per author)", authors[0])
	}
	if authors[1].Author != "Ann Leckie" || authors[1].Count != 1 {
		t.Fatalf("authors[1] = %+v, want Ann Leckie:1", authors[1])
	}
}

func TestTopAuthorsRespectsLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	for i := 0; i < 3; i++ {
		b := seedBook(t, s, "vol-"+string(rune('a'+i)), "Book", []string{"Author " + string(rune('A'+i))})
		seedRead(t, s, userID, b, datedAt("2026-01-01T00:00:00Z"))
	}

	authors, err := s.TopAuthors(ctx, userID, "all", 2)
	if err != nil {
		t.Fatalf("TopAuthors: %v", err)
	}
	if len(authors) != 2 {
		t.Fatalf("len(authors) = %d, want 2", len(authors))
	}
}

func TestStreakCountsBackFromLastCompletedMonth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC) // "now" is August

	jul := seedBook(t, s, "vol-jul", "July", nil)
	jun := seedBook(t, s, "vol-jun", "June", nil)
	may := seedBook(t, s, "vol-may", "May", nil)
	mar := seedBook(t, s, "vol-mar", "March", nil)

	seedRead(t, s, userID, jul, datedAt("2026-07-10T00:00:00Z"))
	seedRead(t, s, userID, jun, datedAt("2026-06-10T00:00:00Z"))
	seedRead(t, s, userID, may, datedAt("2026-05-10T00:00:00Z"))
	seedRead(t, s, userID, mar, datedAt("2026-03-10T00:00:00Z")) // gap at April breaks the streak

	got, err := s.Streak(ctx, userID, now)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 3 {
		t.Fatalf("Streak = %d, want 3", got)
	}
}

func TestStreakIgnoresTheInProgressMonth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	b := seedBook(t, s, "vol-1", "Book", nil)
	seedRead(t, s, userID, b, datedAt("2026-08-01T00:00:00Z")) // this month, not counted

	got, err := s.Streak(ctx, userID, now)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 0 {
		t.Fatalf("Streak = %d, want 0 (in-progress month must not count)", got)
	}
}

func TestStreakCrossesAYearBoundary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	dec := seedBook(t, s, "vol-dec", "December", nil)
	nov := seedBook(t, s, "vol-nov", "November", nil)
	seedRead(t, s, userID, dec, datedAt("2025-12-10T00:00:00Z"))
	seedRead(t, s, userID, nov, datedAt("2025-11-10T00:00:00Z"))

	got, err := s.Streak(ctx, userID, now)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 2 {
		t.Fatalf("Streak = %d, want 2", got)
	}
}

func TestCrossUserIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	bookA := seedBook(t, s, "vol-a", "A", []string{"Author A"})
	bookB := seedBook(t, s, "vol-b", "B", []string{"Author B"})
	seedRead(t, s, a, bookA, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, b, bookB, datedAt("2026-01-01T00:00:00Z"))

	got, err := s.Summary(ctx, a, now)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.TotalRead != 1 {
		t.Fatalf("a's TotalRead = %d, want 1 (b's book must not count)", got.TotalRead)
	}

	authors, err := s.TopAuthors(ctx, a, "all", 5)
	if err != nil {
		t.Fatalf("TopAuthors: %v", err)
	}
	if len(authors) != 1 || authors[0].Author != "Author A" {
		t.Fatalf("authors = %+v, want only Author A", authors)
	}
}

// setBookLanguage is a direct SQL update: UpsertBooks does not expose a way
// to set language on a book seeded through seedBook's authors-only path,
// and stats tests need to distinguish "English" from "no language set".
func setBookLanguage(t *testing.T, s *SQLite, bookID int64, language string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE books SET language = ? WHERE id = ?`, language, bookID); err != nil {
		t.Fatalf("setBookLanguage: %v", err)
	}
}
