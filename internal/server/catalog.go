package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-local-audiobooks/internal/store"
)

type listEnvelope struct {
	Items      []audiobookSummary `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type audiobookSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Narrator    string `json:"narrator,omitempty"`
	Year        string `json:"year,omitempty"`
	Genre       string `json:"genre,omitempty"`
	DurationMs  int64  `json:"duration_ms"`
	LibraryID   int64  `json:"library_id,omitempty"`
	LibraryName string `json:"library_name,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
}

type libraryInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"media_type,omitempty"`
}

func toSummary(a *store.Audiobook, libraryNames map[int64]string) audiobookSummary {
	return audiobookSummary{
		ID:          a.ID,
		Title:       a.Title,
		Author:      a.Author,
		Narrator:    a.Narrator,
		Year:        a.Year,
		Genre:       a.Genre,
		DurationMs:  a.DurationMs,
		LibraryID:   a.LibraryPathID,
		LibraryName: libraryNames[a.LibraryPathID],
		MediaType:   "audiobook",
	}
}

func libraryName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == string(filepath.Separator) {
		return path
	}
	return name
}

func (s *Server) libraryNameMap(ctxReq *http.Request) map[int64]string {
	paths, err := s.deps.Store.ListLibraryPaths(ctxReq.Context())
	if err != nil {
		return nil
	}
	out := make(map[int64]string, len(paths))
	for _, lp := range paths {
		out[lp.ID] = libraryName(lp.Path)
	}
	return out
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

func parseLibraryPathID(r *http.Request) int64 {
	raw := r.URL.Query().Get("library_id")
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func (s *Server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	paths, err := s.deps.Store.ListLibraryPaths(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := struct {
		Items []libraryInfo `json:"items"`
	}{Items: make([]libraryInfo, 0, len(paths))}
	for _, lp := range paths {
		if !lp.Enabled {
			continue
		}
		out.Items = append(out.Items, libraryInfo{
			ID:        lp.ID,
			Name:      libraryName(lp.Path),
			MediaType: "audiobook",
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	books, err := s.deps.Store.ListActiveAudiobooks(r.Context(), store.ListAudiobooksParams{
		Cursor:        q.Get("cursor"),
		Limit:         parseLimit(q.Get("limit"), 50),
		Sort:          q.Get("sort"),
		Order:         q.Get("order"),
		LibraryPathID: parseLibraryPathID(r),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	libraryNames := s.libraryNameMap(r)
	out := listEnvelope{Items: make([]audiobookSummary, 0, len(books))}
	for _, a := range books {
		out.Items = append(out.Items, toSummary(a, libraryNames))
	}
	if len(books) > 0 {
		out.NextCursor = store.EncodeCursor(books[len(books)-1], q.Get("sort"))
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
		Cursor:        q.Get("cursor"),
		Limit:         parseLimit(q.Get("limit"), 50),
		LibraryPathID: parseLibraryPathID(r),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	libraryNames := s.libraryNameMap(r)
	out := listEnvelope{Items: make([]audiobookSummary, 0, len(books))}
	for _, a := range books {
		out.Items = append(out.Items, toSummary(a, libraryNames))
	}
	if len(books) > 0 {
		out.NextCursor = store.EncodeCursor(books[len(books)-1], q.Get("sort"))
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
		"item":     toSummary(book, s.libraryNameMap(r)),
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
