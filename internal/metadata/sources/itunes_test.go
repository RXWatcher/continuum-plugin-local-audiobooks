package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// emptyITunesEnvelope is returned for IDs that have no results.
const emptyITunesEnvelope = `{"resultCount":0,"results":[]}`

func newITunesFake(t *testing.T) (*httptest.Server, *ITunes) {
	t.Helper()
	lookup := loadFixture(t, "itunes_lookup.json")
	search := loadFixture(t, "itunes_search.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/lookup":
			id := r.URL.Query().Get("id")
			if id == "0000000000" {
				w.Write([]byte(emptyITunesEnvelope))
				return
			}
			w.Write(lookup)
		case r.URL.Path == "/search":
			w.Write(search)
		default:
			w.WriteHeader(404)
		}
	}))
	it := NewITunesAt(srv.URL, "test")
	it.http.Client = srv.Client()
	return srv, it
}

func TestITunes_GetByID(t *testing.T) {
	srv, it := newITunesFake(t)
	defer srv.Close()

	c, err := it.Get(context.Background(), "123456789", "us")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil candidate")
	}
	if c.Title != "Project Hail Mary" {
		t.Errorf("title = %q, want %q", c.Title, "Project Hail Mary")
	}
	if c.ExternalID != "123456789" {
		t.Errorf("external_id = %q, want %q", c.ExternalID, "123456789")
	}
	if c.Source != itunesID {
		t.Errorf("source = %q, want %q", c.Source, itunesID)
	}
	// 58200000 ms ÷ 60000 = 970 minutes
	if c.RuntimeMin != 970 {
		t.Errorf("runtime_min = %d, want 970", c.RuntimeMin)
	}
	if !strings.HasPrefix(c.PublishedAt, "2021-05-04") {
		t.Errorf("published_at = %q, want prefix %q", c.PublishedAt, "2021-05-04")
	}
	if len(c.Authors) != 1 || c.Authors[0] != "Andy Weir" {
		t.Errorf("authors = %v, want [Andy Weir]", c.Authors)
	}
}

func TestITunes_GetEmptyResultsAsNotFound(t *testing.T) {
	srv, it := newITunesFake(t)
	defer srv.Close()

	c, err := it.Get(context.Background(), "0000000000", "us")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if c != nil {
		t.Errorf("expected nil candidate, got %+v", c)
	}
}

func TestITunes_SearchByText(t *testing.T) {
	srv, it := newITunesFake(t)
	defer srv.Close()

	cs, err := it.Search(context.Background(), "audiobook", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) < 1 {
		t.Fatalf("got %d candidates, want ≥1", len(cs))
	}
	if cs[0].Title != "Project Hail Mary" {
		t.Errorf("first title = %q, want %q", cs[0].Title, "Project Hail Mary")
	}
}

func TestITunes_ASINQueriesReturnNil(t *testing.T) {
	srv, it := newITunesFake(t)
	defer srv.Close()

	// Get with an ASIN must return (nil, nil).
	c, err := it.Get(context.Background(), "B08G9PRS1K", "us")
	if err != nil {
		t.Errorf("Get ASIN: unexpected error: %v", err)
	}
	if c != nil {
		t.Errorf("Get ASIN: expected nil candidate, got %+v", c)
	}

	// Search with an ASIN must return (nil, nil).
	cs, err := it.Search(context.Background(), "B08G9PRS1K", "us")
	if err != nil {
		t.Errorf("Search ASIN: unexpected error: %v", err)
	}
	if cs != nil {
		t.Errorf("Search ASIN: expected nil slice, got %v", cs)
	}
}
