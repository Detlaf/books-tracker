package books

import "context"

// Provider is the external metadata source. Implementations return Books with
// ID left zero — local identity belongs to the store, not the provider — which
// is what lets the service swap a provider without touching persistence.
type Provider interface {
	Search(ctx context.Context, q string, page, limit int) ([]Book, error)
	ByISBN(ctx context.Context, isbn string) (Book, error)
}
