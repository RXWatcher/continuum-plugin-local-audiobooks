package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/store"
)

func (s *Server) handleAdminListPaths(w http.ResponseWriter, r *http.Request) {
	paths, err := s.deps.Store.ListLibraryPaths(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": paths})
}

type addPathReq struct {
	Path string `json:"path"`
}

func (s *Server) handleAdminAddPath(w http.ResponseWriter, r *http.Request) {
	var body addPathReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(body.Path)
	if err != nil || !info.IsDir() {
		http.Error(w, "path is not a readable directory", http.StatusBadRequest)
		return
	}
	row, err := s.deps.Store.UpsertLibraryPath(r.Context(), body.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleAdminDeletePath(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.deps.Store.DeleteLibraryPath(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminScan(w http.ResponseWriter, r *http.Request) {
	if s.deps.Scan == nil {
		http.Error(w, "scan not configured", http.StatusServiceUnavailable)
		return
	}
	eventID, err := s.deps.Scan(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"scan_event_id": eventID})
}

func (s *Server) handleAdminScanStatus(w http.ResponseWriter, r *http.Request) {
	events, err := s.deps.Store.ListRecentScanEvents(r.Context(), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

// handleMetadataBackfill enqueues a metadata_enrichment_job for every
// audiobook that has no existing enrichment job. Returns {"queued": <count>}.
func (s *Server) handleMetadataBackfill(w http.ResponseWriter, r *http.Request) {
	if s.deps.MetadataQueue == nil {
		http.Error(w, `{"error":"metadata queue not configured"}`, http.StatusServiceUnavailable)
		return
	}
	ids, err := s.deps.Store.ListPendingBackfill(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	for _, id := range ids {
		_ = s.deps.MetadataQueue.Enqueue(r.Context(), id) // best-effort
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"queued": len(ids)})
}
