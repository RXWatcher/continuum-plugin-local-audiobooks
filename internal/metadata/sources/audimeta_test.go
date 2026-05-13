package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newAudiMetaFake(t *testing.T) (*httptest.Server, *AudiMeta) {
	t.Helper()
	book := loadFixture(t, "audimeta_book.json")
	search := loadFixture(t, "audimeta_search.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/books/B08G9PRS1K":
			w.Write(book)
		case r.URL.Path == "/books/B0MISSING0":
			w.WriteHeader(404)
		case strings.HasPrefix(r.URL.Path, "/search"):
			w.Write(search)
		default:
			w.WriteHeader(404)
		}
	}))
	a := NewAudiMetaAt(srv.URL, "test")
	a.http.Client = srv.Client()
	return srv, a
}

func TestAudiMeta_GetByASIN(t *testing.T) {
	srv, a := newAudiMetaFake(t)
	defer srv.Close()
	c, err := a.Get(context.Background(), "B08G9PRS1K", "us")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil candidate")
	}
	if c.Title != "Project Hail Mary" {
		t.Errorf("title %q", c.Title)
	}
	if c.ASIN != "B08G9PRS1K" {
		t.Errorf("asin %q", c.ASIN)
	}
	if c.Source != "audimeta" {
		t.Errorf("source %q", c.Source)
	}
	if c.RuntimeMin != 970 {
		t.Errorf("runtime %d", c.RuntimeMin)
	}
	if c.Series != "Hail Mary" || c.SeriesPos != "1" {
		t.Errorf("series %q %q", c.Series, c.SeriesPos)
	}
	if len(c.Authors) != 1 || c.Authors[0] != "Andy Weir" {
		t.Errorf("authors %v", c.Authors)
	}
	if len(c.Narrators) != 1 || c.Narrators[0] != "Ray Porter" {
		t.Errorf("narrators %v", c.Narrators)
	}
}

func TestAudiMeta_GetMissing(t *testing.T) {
	srv, a := newAudiMetaFake(t)
	defer srv.Close()
	c, err := a.Get(context.Background(), "B0MISSING0", "us")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if c != nil {
		t.Errorf("expected nil candidate")
	}
}

func TestAudiMeta_SearchByText(t *testing.T) {
	srv, a := newAudiMetaFake(t)
	defer srv.Close()
	cs, err := a.Search(context.Background(), "Project Hail Mary", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) < 1 {
		t.Fatalf("got %d candidates, want ≥1", len(cs))
	}
	if cs[0].Title != "Project Hail Mary" {
		t.Errorf("first title %q", cs[0].Title)
	}
}

func TestAudiMeta_SearchByASIN(t *testing.T) {
	srv, a := newAudiMetaFake(t)
	defer srv.Close()
	cs, err := a.Search(context.Background(), "B08G9PRS1K", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cs))
	}
	if cs[0].ASIN != "B08G9PRS1K" {
		t.Errorf("asin %q", cs[0].ASIN)
	}
}
