package library

import (
	"errors"
	"testing"
)

func TestParseStatusAcceptsTheThreeStatuses(t *testing.T) {
	for _, want := range []Status{StatusBacklog, StatusReading, StatusRead} {
		got, err := ParseStatus(string(want))
		if err != nil {
			t.Fatalf("ParseStatus(%q): %v", want, err)
		}
		if got != want {
			t.Fatalf("ParseStatus(%q) = %q", want, got)
		}
	}
}

// A typo must be an error rather than a silently-ignored filter.
func TestParseStatusRejectsAnythingElse(t *testing.T) {
	for _, in := range []string{"", "READ", "finished", "backlog "} {
		if _, err := ParseStatus(in); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("ParseStatus(%q) err = %v, want ErrInvalidStatus", in, err)
		}
	}
}

func TestParseSortAcceptsEveryKeyInBothDirections(t *testing.T) {
	want := []Sort{
		SortAddedAt, SortAddedAtDesc,
		SortFinishedAt, SortFinishedAtDesc,
		SortTitle, SortTitleDesc,
	}
	for _, w := range want {
		got, err := ParseSort(string(w))
		if err != nil {
			t.Fatalf("ParseSort(%q): %v", w, err)
		}
		if got != w {
			t.Fatalf("ParseSort(%q) = %q", w, got)
		}
	}
}

func TestParseSortEmptyIsTheDefault(t *testing.T) {
	got, err := ParseSort("")
	if err != nil {
		t.Fatalf("ParseSort(\"\"): %v", err)
	}
	if got != DefaultSort {
		t.Fatalf("ParseSort(\"\") = %q, want %q", got, DefaultSort)
	}
	if DefaultSort != SortAddedAtDesc {
		t.Fatalf("DefaultSort = %q, want -added_at", DefaultSort)
	}
}

func TestParseSortRejectsUnknownKeys(t *testing.T) {
	for _, in := range []string{"author", "-author", "added", "+added_at", "added_at desc"} {
		if _, err := ParseSort(in); !errors.Is(err, ErrInvalidSort) {
			t.Fatalf("ParseSort(%q) err = %v, want ErrInvalidSort", in, err)
		}
	}
}
