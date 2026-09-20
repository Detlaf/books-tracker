package collections

import (
	"context"
	"errors"
	"testing"
)

// fakeStore holds one user's collections in a map keyed by collection ID.
type fakeStore struct {
	items  map[int64]Collection
	nextID int64
	err    error
}

func newFakeStore() *fakeStore { return &fakeStore{items: map[int64]Collection{}, nextID: 1} }

func (f *fakeStore) Create(_ context.Context, userID int64, name string) (Collection, error) {
	if f.err != nil {
		return Collection{}, f.err
	}
	c := Collection{ID: f.nextID, UserID: userID, Name: name, BookIDs: []int64{}}
	f.items[c.ID] = c
	f.nextID++
	return c, nil
}

func (f *fakeStore) List(_ context.Context, userID int64) ([]Collection, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []Collection
	for _, c := range f.items {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) Rename(_ context.Context, userID, id int64, name string) (Collection, error) {
	if f.err != nil {
		return Collection{}, f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return Collection{}, ErrNotFound
	}
	c.Name = name
	f.items[id] = c
	return c, nil
}

func (f *fakeStore) Delete(_ context.Context, userID, id int64) error {
	if f.err != nil {
		return f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func (f *fakeStore) AddBook(_ context.Context, userID, id, bookID int64) (Collection, error) {
	if f.err != nil {
		return Collection{}, f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return Collection{}, ErrNotFound
	}
	for _, b := range c.BookIDs {
		if b == bookID {
			return c, nil
		}
	}
	c.BookIDs = append(c.BookIDs, bookID)
	f.items[id] = c
	return c, nil
}

func (f *fakeStore) RemoveBook(_ context.Context, userID, id, bookID int64) error {
	if f.err != nil {
		return f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	idx := -1
	for i, b := range c.BookIDs {
		if b == bookID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrBookNotInCollection
	}
	c.BookIDs = append(c.BookIDs[:idx], c.BookIDs[idx+1:]...)
	f.items[id] = c
	return nil
}

func newTestService(store Store) *Service { return NewService(store) }

func TestCreateTrimsName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.Create(context.Background(), 1, "  Summer reading  ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Name != "Summer reading" {
		t.Fatalf("Name = %q, want trimmed", got.Name)
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	for _, name := range []string{"", "   "} {
		_, err := svc.Create(context.Background(), 1, name)
		if !errors.Is(err, ErrEmptyName) {
			t.Fatalf("name %q: err = %v, want ErrEmptyName", name, err)
		}
	}
}

func TestRenameTrimsName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Original")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := svc.Rename(context.Background(), 1, c.ID, "  Renamed  ")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Name != "Renamed" {
		t.Fatalf("Name = %q, want trimmed", got.Name)
	}
}

func TestRenameRejectsEmptyName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Original")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Rename(context.Background(), 1, c.ID, "   ")
	if !errors.Is(err, ErrEmptyName) {
		t.Fatalf("err = %v, want ErrEmptyName", err)
	}
}

func TestRenameMissingIsNotFound(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.Rename(context.Background(), 1, 999, "New name")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListNeverReturnsNil(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.List(context.Background(), 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("List must return an empty slice, not nil")
	}
}

func TestDeleteMissingIsNotFound(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	if err := svc.Delete(context.Background(), 1, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddBookMissingCollectionIsNotFound(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.AddBook(context.Background(), 1, 999, 42)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddBookIsIdempotent(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.AddBook(context.Background(), 1, c.ID, 42); err != nil {
		t.Fatalf("AddBook: %v", err)
	}
	got, err := svc.AddBook(context.Background(), 1, c.ID, 42)
	if err != nil {
		t.Fatalf("AddBook (repeat): %v", err)
	}
	if len(got.BookIDs) != 1 {
		t.Fatalf("BookIDs = %v, want exactly one entry", got.BookIDs)
	}
}

func TestRemoveBookNotInCollectionIsError(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = svc.RemoveBook(context.Background(), 1, c.ID, 42)
	if !errors.Is(err, ErrBookNotInCollection) {
		t.Fatalf("err = %v, want ErrBookNotInCollection", err)
	}
}

func TestRemoveBookRemovesIt(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.AddBook(context.Background(), 1, c.ID, 42); err != nil {
		t.Fatalf("AddBook: %v", err)
	}

	if err := svc.RemoveBook(context.Background(), 1, c.ID, 42); err != nil {
		t.Fatalf("RemoveBook: %v", err)
	}
}
