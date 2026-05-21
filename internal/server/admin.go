package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/store"
)

func (s *Server) handleAdminListPaths(w http.ResponseWriter, r *http.Request) {
	paths, err := s.deps.Store.ListLibraryPaths(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
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
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "path required")
		return
	}
	info, err := os.Stat(body.Path)
	if err != nil || !info.IsDir() {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "path is not a readable directory")
		return
	}
	row, err := s.deps.Store.UpsertLibraryPath(r.Context(), body.Path)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleAdminDeletePath(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "bad id")
		return
	}
	if err := s.deps.Store.DeleteLibraryPath(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeAdminError(w, http.StatusNotFound, "not_found", "library path not found")
		return
	} else if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.deps.Store.GetAppConfig(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleAdminPutConfig(w http.ResponseWriter, r *http.Request) {
	var cfg store.AppConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	cfg = cfg.WithDefaults()
	if cfg.StandaloneHTTPListen != "" && cfg.StreamSigningSecret == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", "stream_signing_secret is required when standalone_http_listen is set")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := s.deps.Store.PutAppConfig(r.Context(), cfg); err != nil {
		writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleAdminScan(w http.ResponseWriter, r *http.Request) {
	if s.deps.Scan == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "not_configured", "scan not configured")
		return
	}
	eventID, err := s.deps.Scan(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "scan_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"scan_event_id": eventID})
}

func (s *Server) handleAdminScanStatus(w http.ResponseWriter, r *http.Request) {
	events, err := s.deps.Store.ListRecentScanEvents(r.Context(), 50)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

// handleMetadataBackfill enqueues a metadata_enrichment_job for every
// audiobook that has no existing enrichment job. Returns {"queued": <count>}.
func (s *Server) handleMetadataBackfill(w http.ResponseWriter, r *http.Request) {
	n, err := s.deps.Store.BulkEnqueueBackfill(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "backfill_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{"queued": n})
}

func writeAdminError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

type filesystemBrowseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type filesystemBrowseResponse struct {
	Path    string                  `json:"path"`
	Parent  string                  `json:"parent"`
	Entries []filesystemBrowseEntry `json:"entries"`
}

func handleAdminFilesystemBrowse(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = string(filepath.Separator)
	}
	if !filepath.IsAbs(path) {
		writeAdminError(w, http.StatusBadRequest, "bad_request", "path must be an absolute path")
		return
	}

	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			writeAdminError(w, http.StatusNotFound, "not_found", "directory not found")
		} else {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid path")
		}
		return
	}
	if !info.IsDir() {
		writeAdminError(w, http.StatusBadRequest, "bad_request", "path must point to a directory")
		return
	}

	entries, err := os.ReadDir(cleaned)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "failed to read directory")
		return
	}

	result := make([]filesystemBrowseEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		result = append(result, filesystemBrowseEntry{Name: name, Path: filepath.Join(cleaned, name)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].Path < result[j].Path
		}
		return result[i].Name < result[j].Name
	})

	parent := filepath.Dir(cleaned)
	if cleaned == string(filepath.Separator) || parent == "." || parent == cleaned {
		parent = cleaned
	}
	writeJSON(w, http.StatusOK, filesystemBrowseResponse{Path: cleaned, Parent: parent, Entries: result})
}
