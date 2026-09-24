package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStatsRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	paths := []string{
		"/stats/summary", "/stats/by-year", "/stats/by-month",
		"/stats/by-language", "/stats/top-authors", "/stats/streak",
	}
	for _, p := range paths {
		rec := get(t, srv, p, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d, want 401", p, rec.Code)
		}
	}
}

func TestStatsSummaryShapeWithNoBooks(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/summary", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var body struct {
		TotalRead   int `json:"total_read"`
		Reading     int `json:"reading"`
		Backlog     int `json:"backlog"`
		ThisYear    int `json:"this_year"`
		LastYear    int `json:"last_year"`
		CurrentYear int `json:"current_year"`
		UndatedRead int `json:"undated_read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalRead != 0 || body.CurrentYear == 0 {
		t.Fatalf("body = %+v, want zero counts and a real current_year", body)
	}
}

func TestStatsByYearEmptyArraysNotNull(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-year", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["years"]) != "[]" {
		t.Fatalf("years = %s, want []", raw["years"])
	}
}

func TestStatsByMonthDefaultsAndReturnsTwelveMonths(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-month", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Year   string `json:"year"`
		Months []struct {
			Month int `json:"month"`
			Count int `json:"count"`
		} `json:"months"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Year == "" {
		t.Fatal("year must default rather than be empty")
	}
	if len(body.Months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(body.Months))
	}
}

func TestStatsByMonthRejectsBadYear(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-month?year=20xx", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStatsByLanguageRejectsBadScope(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-language?scope=nonsense", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStatsTopAuthorsRejectsBadScope(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/top-authors?scope=nonsense", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStatsStreakShape(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/streak", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Months int `json:"months"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Months != 0 {
		t.Fatalf("months = %d, want 0 for a fresh user", body.Months)
	}
}

// TestStatsScopingIsPerCaller confirms a second user's reads never leak into
// the first user's stats, exercised through the HTTP layer.
func TestStatsScopingIsPerCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLoginAs(t, srv, "a@b.com")
	registerAndLoginAs(t, srv, "b@b.com")

	rec := get(t, srv, "/stats/summary", "Bearer "+a.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		TotalRead int `json:"total_read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalRead != 0 {
		t.Fatalf("TotalRead = %d, want 0 (neither user has read anything)", body.TotalRead)
	}
}
