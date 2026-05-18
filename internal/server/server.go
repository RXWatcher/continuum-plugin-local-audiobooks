// Package server builds the chi handler for /api/v1 and /admin routes.
package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-local-audiobooks/internal/store"
)

// EnrichmentQueue is the surface the server needs from metadata.Queue.
// Declared as an interface to avoid an import cycle
// (metadata → store, store → server would cycle).
type EnrichmentQueue interface {
	Enqueue(ctx context.Context, audiobookID string) error
}

// Deps holds the handler's collaborators.
type Deps struct {
	Store        *store.Store
	StandaloneOn bool   // true when serving on the standalone listener
	StreamSecret []byte // shared HMAC for stream-token verification

	// Scan triggers a library scan. Returns the scan_event id. Multiple
	// concurrent calls de-duplicate to the same in-flight id. Nil-safe (the
	// admin handler returns 503 when Scan is nil).
	Scan func(context.Context) (int64, error)

	// MetadataQueue is optional. When non-nil, the /admin/metadata/backfill
	// endpoint enqueues enrichment jobs for all audiobooks lacking one.
	MetadataQueue EnrichmentQueue
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
	r.Get("/api/v1/catalog/libraries", s.handleListLibraries)
	r.Get("/api/v1/catalog/search", s.handleSearchCatalog)
	r.Get("/api/v1/catalog/{id}", s.handleGetCatalog)
	r.Get("/api/v1/browse/authors", s.handleBrowseAuthors)
	r.Get("/api/v1/browse/genres", s.handleBrowseGenres)
	r.Get("/api/v1/cover/{id}/{size}", s.handleCover)
	r.Get("/api/v1/file/{id}", s.handleFile)
	r.Get("/api/v1/requests/{externalId}", s.handleRequestsStub)
	r.Get("/admin", s.handleAdminHome)
	r.Get("/admin/", s.handleAdminHome)
	r.Post("/admin/scan", s.handleAdminScan)
	r.Get("/admin/scan/status", s.handleAdminScanStatus)
	r.Get("/admin/library-paths", s.handleAdminListPaths)
	r.Post("/admin/library-paths", s.handleAdminAddPath)
	r.Delete("/admin/library-paths/{id}", s.handleAdminDeletePath)
	r.Post("/admin/metadata/backfill", s.handleMetadataBackfill)
	return r
}

func (s *Server) handleAdminHome(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Local Audiobooks</title></head>
<body style="font-family:system-ui,sans-serif;margin:32px;line-height:1.5;background:#111;color:#eee">
<h1>Local Audiobooks</h1>
<p>Local audiobook library source, scanner, metadata enrichment, cover, and file routes.</p>
<ul>
<li><a style="color:#8ab4f8" href="./admin/scan/status">Scan status</a></li>
<li><a style="color:#8ab4f8" href="./admin/library-paths">Library paths</a></li>
</ul>
</body></html>`))
}
