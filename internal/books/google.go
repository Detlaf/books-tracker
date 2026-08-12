package books

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	googleVolumesURL = "https://www.googleapis.com/books/v1/volumes"
	// DefaultSearchLimit is the page size when a caller does not choose one.
	DefaultSearchLimit = 20
	// MaxSearchLimit is Google's own maxResults ceiling; asking for more is an
	// error upstream, so the client clamps instead of forwarding it.
	MaxSearchLimit = 40
	// requestTimeout bounds a single upstream call. A search that has not
	// answered in five seconds is not worth holding a client request open for.
	requestTimeout = 5 * time.Second
)

// GoogleBooks queries the Google Books volumes API. baseURL is a field rather
// than a constant so tests can point it at an httptest server; nothing in
// production sets it.
type GoogleBooks struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewGoogleBooks(apiKey string) *GoogleBooks {
	return &GoogleBooks{
		baseURL: googleVolumesURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: requestTimeout},
	}
}

func (g *GoogleBooks) Search(ctx context.Context, q string, page, limit int) ([]Book, error) {
	if limit < 1 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	if page < 1 {
		page = 1
	}
	return g.fetch(ctx, url.Values{
		"q":          {q},
		"maxResults": {strconv.Itoa(limit)},
		"startIndex": {strconv.Itoa((page - 1) * limit)},
	})
}

// ByISBN takes the first volume for the ISBN. Editions occasionally share an
// ISBN upstream; the first result is Google's own relevance pick.
func (g *GoogleBooks) ByISBN(ctx context.Context, isbn string) (Book, error) {
	found, err := g.fetch(ctx, url.Values{
		"q":          {"isbn:" + isbn},
		"maxResults": {"1"},
	})
	if err != nil {
		return Book{}, err
	}
	if len(found) == 0 {
		return Book{}, ErrNotFound
	}
	return found[0], nil
}

// fetch performs one volumes query. Every failure is wrapped into ErrUpstream
// or ErrRateLimited: the detail is for logs only, because a Google error body
// can echo the request URL and the request URL carries the API key.
func (g *GoogleBooks) fetch(ctx context.Context, params url.Values) ([]Book, error) {
	if g.apiKey != "" {
		params.Set("key", g.apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUpstream, err)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}

	var list volumeList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}

	out := make([]Book, 0, len(list.Items))
	for _, v := range list.Items {
		if b, ok := v.toBook(); ok {
			out = append(out, b)
		}
	}
	return out, nil
}

type volumeList struct {
	Items []volume `json:"items"`
}

type volume struct {
	ID         string `json:"id"`
	VolumeInfo struct {
		Title      string   `json:"title"`
		Authors    []string `json:"authors"`
		Language   string   `json:"language"`
		ImageLinks struct {
			Thumbnail string `json:"thumbnail"`
		} `json:"imageLinks"`
		IndustryIdentifiers []struct {
			Type       string `json:"type"`
			Identifier string `json:"identifier"`
		} `json:"industryIdentifiers"`
	} `json:"volumeInfo"`
}

// toBook reports false for a volume that cannot be stored. A missing title
// violates books.title's NOT NULL constraint and a missing ID leaves the row
// with no dedupe key, so both are dropped rather than filled with a
// placeholder that would later look like real metadata.
func (v volume) toBook() (Book, bool) {
	if v.ID == "" || strings.TrimSpace(v.VolumeInfo.Title) == "" {
		return Book{}, false
	}
	return Book{
		ExternalID: v.ID,
		ISBN:       v.isbn(),
		Title:      v.VolumeInfo.Title,
		Authors:    v.VolumeInfo.Authors,
		Language:   v.VolumeInfo.Language,
		CoverURL:   v.VolumeInfo.ImageLinks.Thumbnail,
		Source:     SourceGoogleBooks,
	}, true
}

// isbn prefers ISBN_13 and falls back to ISBN_10, which is all some older
// volumes carry. Neither present leaves the field empty.
func (v volume) isbn() string {
	var fallback string
	for _, id := range v.VolumeInfo.IndustryIdentifiers {
		switch id.Type {
		case "ISBN_13":
			return id.Identifier
		case "ISBN_10":
			fallback = id.Identifier
		}
	}
	return fallback
}
