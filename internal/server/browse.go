package server

import "net/http"

type facetEnvelope struct {
	Items      []facetItem `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

type facetItem struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func (s *Server) handleBrowseAuthors(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.deps.Store.ListAuthorsWithCounts(r.Context(),
		q.Get("cursor"),
		parseLimit(q.Get("limit"), 100),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := facetEnvelope{Items: make([]facetItem, 0, len(rows))}
	for _, f := range rows {
		out.Items = append(out.Items, facetItem{Name: f.Value, Count: f.Count})
	}
	if len(rows) > 0 {
		out.NextCursor = rows[len(rows)-1].Value
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBrowseGenres(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.deps.Store.ListGenresWithCounts(r.Context(),
		q.Get("cursor"),
		parseLimit(q.Get("limit"), 100),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := facetEnvelope{Items: make([]facetItem, 0, len(rows))}
	for _, f := range rows {
		out.Items = append(out.Items, facetItem{Name: f.Value, Count: f.Count})
	}
	if len(rows) > 0 {
		out.NextCursor = rows[len(rows)-1].Value
	}
	writeJSON(w, http.StatusOK, out)
}
