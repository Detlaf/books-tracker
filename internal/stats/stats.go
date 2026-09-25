// Package stats computes each user's reading statistics — summary counts,
// yearly/monthly breakdowns, language and author rankings, and reading
// streaks — over their library.
package stats

// Summary is the headline numbers shown at the top of the Reports screen.
// CurrentYear is the server's UTC year, echoed back so the client never has
// to reconcile its own clock against the server's idea of "this year".
type Summary struct {
	TotalRead   int
	Reading     int
	Backlog     int
	ThisYear    int
	LastYear    int
	CurrentYear int
	UndatedRead int
}

// YearCount is one year's count of read books with a finished_at date.
// Year is a string, not an int: it is an opaque label, not a number to do
// arithmetic on, matching how the rest of the codebase treats it.
type YearCount struct {
	Year  string
	Count int
}

// MonthCount is one calendar month's count within a single year. Month is
// 1-12.
type MonthCount struct {
	Month int
	Count int
}

// LanguageCount is one language's count of read books within a scope.
type LanguageCount struct {
	Language string
	Count    int
}

// AuthorCount is one author's count of read books within a scope. A book
// with several authors counts once per author.
type AuthorCount struct {
	Author string
	Count  int
}
