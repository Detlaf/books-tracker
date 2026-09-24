package stats

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeStore records the args each method was called with and returns
// canned results, so Service's validation/defaulting logic can be tested
// without a database.
type fakeStore struct {
	byMonthYear     string
	byLanguageScope string
	topAuthorsScope string
	topAuthorsLimit int
}

func (f *fakeStore) Summary(_ context.Context, _ int64, _ time.Time) (Summary, error) {
	return Summary{TotalRead: 1}, nil
}

func (f *fakeStore) ByYear(_ context.Context, _ int64) ([]YearCount, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) ByMonth(_ context.Context, _ int64, year string) ([]MonthCount, error) {
	f.byMonthYear = year
	return make([]MonthCount, 12), nil
}

func (f *fakeStore) ByLanguage(_ context.Context, _ int64, scope string) ([]LanguageCount, error) {
	f.byLanguageScope = scope
	return nil, nil
}

func (f *fakeStore) TopAuthors(_ context.Context, _ int64, scope string, limit int) ([]AuthorCount, error) {
	f.topAuthorsScope = scope
	f.topAuthorsLimit = limit
	return nil, nil
}

func (f *fakeStore) Streak(_ context.Context, _ int64, _ time.Time) (int, error) {
	return 4, nil
}

func fixedNow() time.Time { return time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC) }

func TestByMonthDefaultsYearToNow(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	year, months, err := svc.ByMonth(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if year != "2026" {
		t.Fatalf("year = %q, want 2026", year)
	}
	if f.byMonthYear != "2026" {
		t.Fatalf("store received year = %q, want 2026", f.byMonthYear)
	}
	if len(months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(months))
	}
}

func TestByMonthRejectsBadYear(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	for _, year := range []string{"20xx", "20266", "202", "all"} {
		if _, _, err := svc.ByMonth(context.Background(), 1, year); !errors.Is(err, ErrInvalidYear) {
			t.Fatalf("year %q: err = %v, want ErrInvalidYear", year, err)
		}
	}
}

func TestByMonthAcceptsValidYear(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	year, _, err := svc.ByMonth(context.Background(), 1, "2024")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if year != "2024" {
		t.Fatalf("year = %q, want 2024", year)
	}
}

func TestByLanguageDefaultsScopeToAll(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	if _, err := svc.ByLanguage(context.Background(), 1, ""); err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if f.byLanguageScope != "all" {
		t.Fatalf("scope = %q, want all", f.byLanguageScope)
	}
}

func TestByLanguageRejectsBadScope(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	for _, scope := range []string{"20xx", "24", "20266", "reading"} {
		if _, err := svc.ByLanguage(context.Background(), 1, scope); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("scope %q: err = %v, want ErrInvalidScope", scope, err)
		}
	}
}

func TestByLanguageAcceptsYearScope(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	if _, err := svc.ByLanguage(context.Background(), 1, "2025"); err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if f.byLanguageScope != "2025" {
		t.Fatalf("scope = %q, want 2025", f.byLanguageScope)
	}
}

func TestTopAuthorsClampsLimit(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, DefaultTopAuthorsLimit},
		{-5, DefaultTopAuthorsLimit},
		{3, 3},
		{50, 50},
		{51, MaxTopAuthorsLimit},
		{10000, MaxTopAuthorsLimit},
	}
	for _, c := range cases {
		f := &fakeStore{}
		svc := NewServiceWithClock(f, fixedNow)
		if _, err := svc.TopAuthors(context.Background(), 1, "all", c.in); err != nil {
			t.Fatalf("in %d: TopAuthors: %v", c.in, err)
		}
		if f.topAuthorsLimit != c.want {
			t.Fatalf("in %d: limit = %d, want %d", c.in, f.topAuthorsLimit, c.want)
		}
	}
}

func TestTopAuthorsRejectsBadScope(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	if _, err := svc.TopAuthors(context.Background(), 1, "not-a-year", 5); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("err = %v, want ErrInvalidScope", err)
	}
}

func TestByYearNeverReturnsNilSlice(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	years, _, err := svc.ByYear(context.Background(), 1)
	if err != nil {
		t.Fatalf("ByYear: %v", err)
	}
	if years == nil {
		t.Fatal("ByYear must return an empty slice, not nil")
	}
}

func TestStreakDelegatesToStoreWithNow(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	got, err := svc.Streak(context.Background(), 1)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 4 {
		t.Fatalf("Streak = %d, want 4", got)
	}
}
