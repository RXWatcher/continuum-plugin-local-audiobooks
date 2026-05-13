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

// newAudiMetaFakeWithSearch creates a fake server that returns the given body
// for any /search request. Used to test different envelope shapes.
func newAudiMetaFakeWithSearch(t *testing.T, searchBody []byte) (*httptest.Server, *AudiMeta) {
	t.Helper()
	book := loadFixture(t, "audimeta_book.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/books/B08G9PRS1K":
			w.Write(book)
		case strings.HasPrefix(r.URL.Path, "/search"):
			w.Write(searchBody)
		default:
			w.WriteHeader(404)
		}
	}))
	a := NewAudiMetaAt(srv.URL, "test")
	a.http.Client = srv.Client()
	return srv, a
}

func TestAudiMeta_SearchEnvelopeShapes(t *testing.T) {
	item := `{"asin":"B08G9PRS1K","title":"Project Hail Mary","authors":[{"name":"Andy Weir"}]}`
	cases := []struct {
		name string
		body string
	}{
		{"books key", `{"books":[` + item + `]}`},
		{"items key", `{"items":[` + item + `]}`},
		{"data key", `{"data":[` + item + `]}`},
		{"raw array", `[` + item + `]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, a := newAudiMetaFakeWithSearch(t, []byte(tc.body))
			defer srv.Close()
			cs, err := a.Search(context.Background(), "Project Hail Mary", "us")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cs) < 1 {
				t.Fatalf("got 0 candidates, want ≥1")
			}
			if cs[0].Title != "Project Hail Mary" {
				t.Errorf("title %q", cs[0].Title)
			}
		})
	}
}

func TestAudiMeta_SearchUnknownEnvelopeErrors(t *testing.T) {
	srv, a := newAudiMetaFakeWithSearch(t, []byte(`{"unexpected":"shape"}`))
	defer srv.Close()
	_, err := a.Search(context.Background(), "something", "us")
	if err == nil {
		t.Fatal("expected error for unrecognized envelope shape, got nil")
	}
}
