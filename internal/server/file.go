package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	book, err := s.deps.Store.GetAudiobook(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f, err := os.Open(book.Path)
	if err != nil {
		http.Error(w, "file not readable", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentTypeFor(book.Path))
	http.ServeContent(w, r, book.Path, book.MTime, f)
}

func (s *Server) handleFileStandalone(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := requireStreamToken(r, s.deps.StreamSecret, id, 0); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	s.handleFile(w, r)
}

func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m4b", ".m4a":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	}
	return "application/octet-stream"
}
