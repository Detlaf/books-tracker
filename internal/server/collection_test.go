package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type collectionBody struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	BookIDs   []int64 `json:"book_ids"`
	CreatedAt string  `json:"created_at"`
}

type collectionListBody struct {
	Items []collectionBody `json:"items"`
}

func TestCollectionRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/collections"},
		{http.MethodPost, "/collections"},
		{http.MethodPatch, "/collections/1"},
		{http.MethodDelete, "/collections/1"},
		{http.MethodPost, "/collections/1/books"},
		{http.MethodDelete, "/collections/1/books/1"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, c.method, c.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestCreateCollectionReturns201(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Summer reading"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var body collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "Summer reading" {
		t.Fatalf("name = %q", body.Name)
	}
	if body.BookIDs == nil || len(body.BookIDs) != 0 {
		t.Fatalf("book_ids = %v, want []", body.BookIDs)
	}
	if body.CreatedAt == "" {
		t.Fatal("created_at must be present")
	}
}

func TestCreateCollectionRejectsEmptyName(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	for _, name := range []string{"", "   "} {
		rec := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
			map[string]any{"name": name})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("name %q: status = %d, want 400: %s", name, rec.Code, rec.Body)
		}
	}
}

func TestListCollectionsReturnsEmptyArrayNotNull(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/collections", "Bearer "+pair.AccessToken)
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

func TestRenameCollectionReturns200(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Original"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/collections/%d", c.ID),
		pair.AccessToken, map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var renamed collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Renamed" {
		t.Fatalf("name = %q, want Renamed", renamed.Name)
	}
}

func TestRenameCollectionMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPatch, "/collections/9999", pair.AccessToken,
		map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestDeleteCollectionReturns204(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Gone soon"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	rec := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/collections/%d", c.ID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
}

func TestDeleteCollectionMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodDelete, "/collections/9999", pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAddBookToCollectionReturns201WithUpdatedList(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		pair.AccessToken, map[string]any{"book_id": bookID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var updated collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.BookIDs) != 1 || updated.BookIDs[0] != bookID {
		t.Fatalf("book_ids = %v, want [%d]", updated.BookIDs, bookID)
	}
}

func TestAddBookToCollectionIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	path := fmt.Sprintf("/collections/%d/books", c.ID)

	if rec := doAuthedJSON(t, srv, http.MethodPost, path, pair.AccessToken,
		map[string]any{"book_id": bookID}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	rec := doAuthedJSON(t, srv, http.MethodPost, path, pair.AccessToken, map[string]any{"book_id": bookID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var updated collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.BookIDs) != 1 {
		t.Fatalf("book_ids = %v, want exactly one entry", updated.BookIDs)
	}
}

func TestAddUnknownBookToCollectionIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	rec := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		pair.AccessToken, map[string]any{"book_id": 9999})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAddBookToMissingCollectionIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/collections/9999/books", pair.AccessToken,
		map[string]any{"book_id": bookID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestRemoveBookFromCollectionReturns204(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		pair.AccessToken, map[string]any{"book_id": bookID}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodDelete,
		fmt.Sprintf("/collections/%d/books/%d", c.ID, bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
}

func TestRemoveBookNotInCollectionIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodDelete,
		fmt.Sprintf("/collections/%d/books/%d", c.ID, bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// Another user's collection must be indistinguishable from one that does
// not exist: 404 everywhere, never 403, and never visible in a list.
func TestCollectionsAreScopedToTheCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLogin(t, srv)
	b := registerAndLoginAs(t, srv, "b@b.com")
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", a.AccessToken,
		map[string]any{"name": "A's collection"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rename := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/collections/%d", c.ID),
		b.AccessToken, map[string]any{"name": "Stolen"})
	if rename.Code != http.StatusNotFound {
		t.Fatalf("cross-user rename = %d, want 404: %s", rename.Code, rename.Body)
	}
	del := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/collections/%d", c.ID), b.AccessToken, nil)
	if del.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete = %d, want 404: %s", del.Code, del.Body)
	}
	add := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		b.AccessToken, map[string]any{"book_id": bookID})
	if add.Code != http.StatusNotFound {
		t.Fatalf("cross-user add book = %d, want 404: %s", add.Code, add.Body)
	}

	list := get(t, srv, "/collections", "Bearer "+b.AccessToken)
	var listBody collectionListBody
	if err := json.Unmarshal(list.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Items) != 0 {
		t.Fatalf("user B sees %d of user A's collections", len(listBody.Items))
	}
}
