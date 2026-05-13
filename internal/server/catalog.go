package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/store"
)

type listEnvelope struct {
	Items      []audiobookSummary `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type audiobookSummary struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Narrator   string `json:"narrator,omitempty"`
	Year       string `json:"year,omitempty"`
	Genre      string `json:"genre,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

func toSummary(a *store.Audiobook) audiobookSummary {
	return audiobookSummary{
		ID:         a.ID,
		Title:      a.Title,
		Author:     a.Author,
		Narrator:   a.Narrator,
		Year:       a.Year,
		Genre:      a.Genre,
		DurationMs: a.DurationMs,
	}
}

func parseLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > 200 {
		return 200
	}
	return n
}

func (s *Server) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	books, err := s.deps.Store.ListActiveAudiobooks(r.Context(), store.ListAudiobooksParams{
		Cursor: q.Get("cursor"),
		Limit:  parseLimit(q.Get("limit"), 50),
		Sort:   q.Get("sort"),
		Order:  q.Get("order"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := listEnvelope{Items: make([]audiobookSummary, 0, len(books))}
	for _, a := range books {
		out.Items = append(out.Items, toSummary(a))
	}
	if len(books) > 0 {
		out.NextCursor = books[len(books)-1].ID
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSearchCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, listEnvelope{Items: []audiobookSummary{}})
		return
	}
	books, err := s.deps.Store.SearchAudiobooks(r.Context(), query, store.ListAudiobooksParams{
		Cursor: q.Get("cursor"),
		Limit:  parseLimit(q.Get("limit"), 50),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := listEnvelope{Items: make([]audiobookSummary, 0, len(books))}
	for _, a := range books {
		out.Items = append(out.Items, toSummary(a))
	}
	if len(books) > 0 {
		out.NextCursor = books[len(books)-1].ID
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetCatalog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	book, err := s.deps.Store.GetAudiobook(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	chapters, err := s.deps.Store.ListChapters(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item":     toSummary(book),
		"chapters": chapters,
		"files": []map[string]any{
			{"index": 0, "format": "m4b", "duration_ms": book.DurationMs, "path": book.Path},
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
