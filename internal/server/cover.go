package server

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/image/draw"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/auth"
	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/store"
)

// coverModTime is fixed because the cover bytes themselves never change
// for a given audiobook id (id is content-derived). Using a zero time
// disables If-Modified-Since on http.ServeContent.
var coverModTime = time.Unix(0, 0).UTC()

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "book_id")
	size := chi.URLParam(r, "size") // "thumb" | "medium" | "original"
	// Cover route is declared public on the host plugin proxy. Auth is
	// enforced here via the signed media token in ?token=. file_idx=-1 is
	// the sentinel for cover tokens (covers don't address a specific file).
	if err := requireStreamToken(r, s.deps.StreamSecret, id, -1); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	cov, err := s.deps.Store.GetCover(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body := cov.Bytes
	contentType := cov.ContentType
	if size == "thumb" || size == "medium" {
		target := 250
		if size == "medium" {
			target = 500
		}
		resized, mime, rerr := resizeCover(cov.Bytes, cov.ContentType, target)
		if rerr == nil {
			body = resized
			contentType = mime
		}
		// On resize error, fall through to original — better to serve the
		// full cover than fail the request.
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "cover", coverModTime, bytes.NewReader(body))
}

func (s *Server) handleCoverStandalone(w http.ResponseWriter, r *http.Request) {
	if err := requireStreamToken(r, s.deps.StreamSecret, chi.URLParam(r, "book_id"), -1); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// handleCover re-validates; harmless and keeps the gate explicit at the
	// auth boundary even if the standalone+main paths diverge later.
	s.handleCover(w, r)
}

// resizeCover decodes the input image, scales the longest edge to target
// pixels (preserving aspect), and re-encodes as JPEG.
// maxCoverPixels bounds the decoded image dimensions. Cover bytes come from
// untrusted library audio files; a tiny crafted PNG/JPEG can declare enormous
// dimensions and make image.Decode (and the RGBA scale buffer) allocate
// gigabytes — a decompression-bomb DoS. 40 MP is far above any real cover.
const maxCoverPixels = 40_000_000

func resizeCover(in []byte, inMIME string, target int) ([]byte, string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(in))
	if err != nil {
		return nil, "", err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxCoverPixels {
		return nil, "", errors.New("cover image dimensions exceed limit")
	}
	src, _, err := image.Decode(bytes.NewReader(in))
	if err != nil {
		return nil, "", err
	}
	b := src.Bounds()
	longEdge := b.Dx()
	if b.Dy() > longEdge {
		longEdge = b.Dy()
	}
	if longEdge <= target {
		return in, inMIME, nil
	}
	ratio := float64(target) / float64(longEdge)
	dstW := int(float64(b.Dx()) * ratio)
	dstH := int(float64(b.Dy()) * ratio)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

func requireStreamToken(r *http.Request, secret []byte, bookID string, fileIdx int) error {
	token := r.URL.Query().Get("token")
	if token == "" {
		return errors.New("missing token")
	}
	_, err := auth.VerifyStreamToken(secret, token, bookID, fileIdx)
	return err
}
