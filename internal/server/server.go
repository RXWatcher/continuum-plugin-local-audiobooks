// Package server builds the chi handler for /api/v1 and /admin routes.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/store"
)

// Deps holds the handler's collaborators.
type Deps struct {
	Store        *store.Store
	StandaloneOn bool   // true when serving on the standalone listener
	StreamSecret []byte // shared HMAC for stream-token verification
}

// Server wraps the chi handler.
type Server struct {
	deps Deps
}

func New(d Deps) *Server { return &Server{deps: d} }

// Handler returns the chi router. When StandaloneOn is true, only file +
// cover endpoints answer; everything else returns 404. All standalone
// content endpoints require a valid stream-token query param (enforced in
// the handlers themselves — see T17).
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	if s.deps.StandaloneOn {
		r.Get("/api/v1/file/{id}", s.handleFileStandalone)
		r.Get("/api/v1/cover/{id}/{size}", s.handleCoverStandalone)
		r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"code":"not_allowed","message":"only file/cover are exposed on standalone listener"}}`, http.StatusNotFound)
		}))
		return r
	}
	r.Get("/api/v1/catalog", s.handleListCatalog)
	r.Get("/api/v1/catalog/search", s.handleSearchCatalog)
	r.Get("/api/v1/catalog/{id}", s.handleGetCatalog)
	r.Get("/api/v1/browse/authors", s.handleBrowseAuthors)
	r.Get("/api/v1/browse/genres", s.handleBrowseGenres)
	r.Get("/api/v1/cover/{id}/{size}", s.handleCover)
	r.Get("/api/v1/file/{id}", s.handleFile)
	r.Get("/api/v1/requests/{externalId}", s.handleRequestsStub)
	r.Post("/admin/scan", s.handleAdminScan)
	r.Get("/admin/scan/status", s.handleAdminScanStatus)
	r.Get("/admin/library-paths", s.handleAdminListPaths)
	r.Post("/admin/library-paths", s.handleAdminAddPath)
	r.Delete("/admin/library-paths/{id}", s.handleAdminDeletePath)
	return r
}

// Placeholder handlers — real implementations land in T17/T18.
func (s *Server) handleCover(w http.ResponseWriter, _ *http.Request)           { http.Error(w, "not yet implemented (T17)", http.StatusNotImplemented) }
func (s *Server) handleCoverStandalone(w http.ResponseWriter, _ *http.Request) { http.Error(w, "not yet implemented (T17)", http.StatusNotImplemented) }
func (s *Server) handleFile(w http.ResponseWriter, _ *http.Request)            { http.Error(w, "not yet implemented (T17)", http.StatusNotImplemented) }
func (s *Server) handleFileStandalone(w http.ResponseWriter, _ *http.Request)  { http.Error(w, "not yet implemented (T17)", http.StatusNotImplemented) }
func (s *Server) handleRequestsStub(w http.ResponseWriter, _ *http.Request)    { http.Error(w, "not yet implemented (T18)", http.StatusNotImplemented) }
func (s *Server) handleAdminScan(w http.ResponseWriter, _ *http.Request)       { http.Error(w, "not yet implemented (T19)", http.StatusNotImplemented) }
func (s *Server) handleAdminScanStatus(w http.ResponseWriter, _ *http.Request) { http.Error(w, "not yet implemented (T18)", http.StatusNotImplemented) }
func (s *Server) handleAdminListPaths(w http.ResponseWriter, _ *http.Request)  { http.Error(w, "not yet implemented (T18)", http.StatusNotImplemented) }
func (s *Server) handleAdminAddPath(w http.ResponseWriter, _ *http.Request)    { http.Error(w, "not yet implemented (T18)", http.StatusNotImplemented) }
func (s *Server) handleAdminDeletePath(w http.ResponseWriter, _ *http.Request) { http.Error(w, "not yet implemented (T18)", http.StatusNotImplemented) }
