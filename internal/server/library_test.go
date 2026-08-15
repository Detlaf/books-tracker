package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/books"
	"github.com/kate/book-tracking/internal/store"
)

type libraryEntryBody struct {
	Book struct {
		ID      int64    `json:"id"`
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
	} `json:"book"`
	Status     string  `json:"status"`
	FinishedAt *string `json:"finished_at"`
	AddedAt    string  `json:"added_at"`
}

type libraryListBody struct {
	Items []libraryEntryBody `json:"items"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
}

// doAuthedJSON is doJSON with a bearer token; the library routes all sit
// behind RequireAuth.
func doAuthedJSON(t *testing.T, srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONWithHeader(t, srv, method, path, body, "Bearer "+token)
}

// seedBookRow puts a book in the database directly, standing in for the
// /books/search call a client would make first.
func seedBookRow(t *testing.T, srv *Server, externalID, title string, authors []string) int64 {
	t.Helper()
	stored, err := store.New(srv.db).UpsertBooks(t.Context(), []books.Book{{
		ExternalID: externalID,
		Title:      title,
		Authors:    authors,
		Source:     books.SourceGoogleBooks,
	}})
	if err != nil {
		t.Fatalf("seed book: %v", err)
	}
	return stored[0].ID
}

func TestLibraryRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/library"},
		{http.MethodPost, "/library"},
		{http.MethodPatch, "/library/1"},
		{http.MethodDelete, "/library/1"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, c.method, c.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestAddToLibraryReturns201WithTheEntry(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", []string{"Frank Herbert"})

	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "reading"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var body libraryEntryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Book.ID != bookID || body.Book.Title != "Dune" {
		t.Fatalf("book = %+v", body.Book)
	}
	if body.Status != "reading" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.AddedAt == "" {
		t.Fatal("added_at must be present")
	}
}

// finished_at must be an explicit null, never absent: a client should not
// have to guess whether the field is missing or the book is unfinished.
func TestAddToLibraryEmitsExplicitNullFinishedAt(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	v, ok := raw["finished_at"]
	if !ok {
		t.Fatal("finished_at must always be present")
	}
	if string(v) != "null" {
		t.Fatalf("finished_at = %s, want null", v)
	}
}

func TestAddToLibraryUnknownBookIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": 9999, "status": "backlog"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// POST adds and PATCH changes; a second POST must not silently reset a
// deliberately-set status.
func TestAddToLibraryDuplicateIs409(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	body := map[string]any{"book_id": bookID, "status": "read"}

	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken, body); rec.Code != http.StatusCreated {
		t.Fatalf("first add = %d: %s", rec.Code, rec.Body)
	}
	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestAddToLibraryRejectsBadInput(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing status", map[string]any{"book_id": bookID}},
		{"bad status", map[string]any{"book_id": bookID, "status": "finished"}},
		{"missing book_id", map[string]any{"status": "backlog"}},
		{"future finished_at", map[string]any{"book_id": bookID, "status": "read", "finished_at": future}},
		{"finished_at with reading", map[string]any{"book_id": bookID, "status": "reading", "finished_at": "2026-01-01T00:00:00Z"}},
		{"unparseable finished_at", map[string]any{"book_id": bookID, "status": "read", "finished_at": "yesterday"}},
	}
	for _, c := range cases {
		rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestUpdateLibraryEntryStampsFinishedAt(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "reading"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/library/%d", bookID),
		pair.AccessToken, map[string]any{"status": "read"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body libraryEntryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "read" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.FinishedAt == nil {
		t.Fatal("moving to read must stamp finished_at")
	}
	if _, err := time.Parse(time.RFC3339, *body.FinishedAt); err != nil {
		t.Fatalf("finished_at %q is not RFC 3339: %v", *body.FinishedAt, err)
	}
}

func TestUpdateLibraryEntryEmptyBodyIs400(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "reading"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/library/%d", bookID),
		pair.AccessToken, map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestUpdateLibraryEntryMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/library/%d", bookID),
		pair.AccessToken, map[string]any{"status": "read"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestUpdateLibraryEntryBadBookIDIs400(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPatch, "/library/not-a-number",
		pair.AccessToken, map[string]any{"status": "read"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestDeleteLibraryEntry(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/library/%d", bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 must have an empty body, got %s", rec.Body)
	}
}

// A removal from a list the user is looking at should not silently no-op:
// that is more likely a bug than a retry.
func TestDeleteLibraryEntryMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodDelete, "/library/9999", pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestListLibraryReturnsPageEnvelope(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	for _, b := range []struct {
		ext, title, status string
	}{
		{"vol-a", "Anathem", "backlog"},
		{"vol-b", "Blindsight", "read"},
	} {
		id := seedBookRow(t, srv, b.ext, b.title, []string{"Someone"})
		if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
			map[string]any{"book_id": id, "status": b.status}); rec.Code != http.StatusCreated {
			t.Fatal(rec.Body)
		}
	}

	rec := get(t, srv, "/library?sort=title", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body libraryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(body.Items))
	}
	if body.Items[0].Book.Title != "Anathem" {
		t.Fatalf("sort=title did not apply: %v", body.Items[0].Book.Title)
	}
	if body.Page != 1 || body.Limit != 20 {
		t.Fatalf("page/limit = %d/%d, want 1/20", body.Page, body.Limit)
	}
}

func TestListLibraryFiltersByStatus(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	backlogID := seedBookRow(t, srv, "vol-a", "Anathem", nil)
	readID := seedBookRow(t, srv, "vol-b", "Blindsight", nil)
	for id, status := range map[int64]string{backlogID: "backlog", readID: "read"} {
		if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
			map[string]any{"book_id": id, "status": status}); rec.Code != http.StatusCreated {
			t.Fatal(rec.Body)
		}
	}

	rec := get(t, srv, "/library?status=read", "Bearer "+pair.AccessToken)
	var body libraryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].Book.ID != readID {
		t.Fatalf("items = %+v, want only the read book", body.Items)
	}
}

// A typo returning 200 [] reads as "you have no books" and sends the client
// looking in the wrong place.
func TestListLibraryRejectsBadStatusAndSort(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	for _, path := range []string{"/library?status=finished", "/library?sort=author"} {
		rec := get(t, srv, path, "Bearer "+pair.AccessToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400: %s", path, rec.Code, rec.Body)
		}
	}
}

func TestListLibraryEmptyIsAnArrayNotNull(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/library", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["items"]) != "[]" {
		t.Fatalf("items = %s, want []", raw["items"])
	}
}

func TestListLibraryCapsLimit(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/library?limit=5000", "Bearer "+pair.AccessToken)
	var body libraryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Limit != 100 {
		t.Fatalf("limit = %d, want it capped at 100", body.Limit)
	}
}

// Another user's entry must be indistinguishable from one that does not
// exist: 404 everywhere, never 403, and never visible in a list.
func TestLibraryIsScopedToTheCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLogin(t, srv)
	b := registerAndLoginAs(t, srv, "b@b.com")
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", a.AccessToken,
		map[string]any{"book_id": bookID, "status": "read"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	path := fmt.Sprintf("/library/%d", bookID)
	patch := doAuthedJSON(t, srv, http.MethodPatch, path, b.AccessToken,
		map[string]any{"status": "backlog"})
	if patch.Code != http.StatusNotFound {
		t.Fatalf("cross-user PATCH = %d, want 404: %s", patch.Code, patch.Body)
	}

	del := doAuthedJSON(t, srv, http.MethodDelete, path, b.AccessToken, nil)
	if del.Code != http.StatusNotFound {
		t.Fatalf("cross-user DELETE = %d, want 404: %s", del.Code, del.Body)
	}

	list := get(t, srv, "/library", "Bearer "+b.AccessToken)
	var body libraryListBody
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("user B sees %d of user A's entries", len(body.Items))
	}

	// User A's entry is untouched by any of it.
	stillThere := get(t, srv, "/library", "Bearer "+a.AccessToken)
	var aBody libraryListBody
	if err := json.Unmarshal(stillThere.Body.Bytes(), &aBody); err != nil {
		t.Fatal(err)
	}
	if len(aBody.Items) != 1 || aBody.Items[0].Status != "read" {
		t.Fatalf("user A's entry changed: %+v", aBody.Items)
	}
}
