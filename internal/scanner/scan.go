package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// Audiobook is the scanner-facing record. Mirrors store.Audiobook closely
// but avoids importing the store package (keeps the scanner testable with
// a fake).
type Audiobook struct {
	ID            string
	LibraryPathID int64
	Path          string
	FileSize      int64
	MTime         time.Time
	Title         string
	Author        string
	Narrator      string
	Album         string
	Year          string
	Genre         string
	ISBN          string
	ASIN          string
	Description   string
	DurationMs    int64
	ScannedAt     time.Time
}

// Cover mirrors store.Cover.
type Cover struct {
	AudiobookID string
	ContentType string
	Bytes       []byte
	Source      string // "embedded" | "sidecar"
}

// EnrichmentEnqueuer is the surface the scanner needs from metadata.Queue.
// Defined as an interface so tests can fake it without a real DB.
type EnrichmentEnqueuer interface {
	Enqueue(ctx context.Context, audiobookID string) error
}

// ScanStore is the subset of the store interface the scanner needs.
type ScanStore interface {
	ListPaths(ctx context.Context, libraryPathID int64) (map[string]string, error)
	Upsert(ctx context.Context, a Audiobook) error
	ReplaceChapters(ctx context.Context, audiobookID string, chs []ParsedChapter) error
	UpsertCover(ctx context.Context, c Cover) error
	SoftDelete(ctx context.Context, audiobookID string) error
}

// WalkParams configures one scan run.
type WalkParams struct {
	LibraryPathID int64
	Root          string
	// EnrichmentQueue is optional. When non-nil, Walk enqueues an enrichment
	// job after every audiobook insert or content-changed update. Enrichment
	// is best-effort: a queue error is logged but does not abort the scan.
	//
	// Inline enrichment (scan_inline_enrich config flag) is intentionally NOT
	// handled here. Task 16 (main.go wiring) will call worker.Drain(ctx) right
	// after Walk returns when that flag is set, draining all just-enqueued jobs
	// synchronously without coupling the scanner to the worker.
	EnrichmentQueue EnrichmentEnqueuer
}

// WalkResult holds per-run counts.
type WalkResult struct {
	Added   int
	Changed int
	Deleted int
}

// supportedExtension returns the parser key to use, or "" if the file is not
// an audiobook we recognize.
func supportedExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".m4b":
		return "m4b"
	case ".mp3":
		return "mp3"
	}
	return ""
}

// parseAudio routes to the correct parser based on extension. Centralizing
// this here keeps Walk format-agnostic and makes adding a third format
// (e.g. flac) a one-case change.
func parseAudio(path string) (*ParsedM4B, error) {
	switch supportedExtension(filepath.Ext(path)) {
	case "m4b":
		return ParseM4B(path)
	case "mp3":
		return ParseMP3(path)
	}
	return nil, fmt.Errorf("unsupported extension: %s", filepath.Ext(path))
}

// Walk recursively scans Root for .m4b / .mp3 files. For each, computes
// the stable id; new ids insert, changed ids re-extract, unchanged ids
// no-op. After the walk, any store path NOT seen this run is soft-deleted.
func Walk(ctx context.Context, store ScanStore, p WalkParams) (WalkResult, error) {
	if p.Root == "" {
		return WalkResult{}, errors.New("Root is empty")
	}
	existing, err := store.ListPaths(ctx, p.LibraryPathID)
	if err != nil {
		return WalkResult{}, fmt.Errorf("list existing: %w", err)
	}
	pathToID := make(map[string]string, len(existing))
	for id, path := range existing {
		pathToID[path] = id
	}

	seenIDs := map[string]struct{}{}
	var res WalkResult

	err = filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if supportedExtension(filepath.Ext(path)) == "" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			// A single unreadable entry (e.g. a dangling symlink, a file
			// removed mid-walk) must not abort the whole library scan.
			slog.WarnContext(ctx, "scan: skip unstattable file", "path", path, "err", err)
			return nil
		}
		mtime := info.ModTime()
		size := info.Size()
		id := StableID(path, size, mtime)

		// Skip unchanged.
		if existingID, ok := pathToID[path]; ok && existingID == id {
			seenIDs[id] = struct{}{}
			return nil
		}

		parsed, err := parseAudio(path)
		if err != nil {
			// One corrupt/unreadable audio file must not abort the scan of
			// the entire library — log and skip it.
			slog.WarnContext(ctx, "scan: skip unparseable file", "path", path, "err", err)
			return nil
		}
		now := time.Now()
		ab := Audiobook{
			ID:            id,
			LibraryPathID: p.LibraryPathID,
			Path:          path,
			FileSize:      size,
			MTime:         mtime,
			Title:         parsed.Title,
			Author:        parsed.Author,
			Narrator:      parsed.Narrator,
			Album:         parsed.Album,
			Year:          parsed.Year,
			Genre:         parsed.Genre,
			ISBN:          parsed.ISBN,
			ASIN:          parsed.ASIN,
			Description:   parsed.Description,
			DurationMs:    parsed.DurationMs,
			ScannedAt:     now,
		}
		if err := store.Upsert(ctx, ab); err != nil {
			return err
		}
		if err := store.ReplaceChapters(ctx, id, parsed.Chapters); err != nil {
			return err
		}
		if len(parsed.CoverBytes) > 0 {
			src := parsed.CoverSource
			if src == "" {
				src = "embedded"
			}
			if err := store.UpsertCover(ctx, Cover{
				AudiobookID: id,
				ContentType: parsed.CoverMIME,
				Bytes:       parsed.CoverBytes,
				Source:      src,
			}); err != nil {
				return err
			}
		}

		if p.EnrichmentQueue != nil {
			if err := p.EnrichmentQueue.Enqueue(ctx, id); err != nil {
				slog.WarnContext(ctx, "enqueue enrichment", "audiobook_id", id, "err", err)
			}
		}

		if _, wasKnown := pathToID[path]; wasKnown {
			res.Changed++
		} else {
			res.Added++
		}
		seenIDs[id] = struct{}{}
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("walk %s: %w", p.Root, err)
	}

	// Soft-delete: every id from "existing" that wasn't seen this walk.
	for id := range existing {
		if _, ok := seenIDs[id]; ok {
			continue
		}
		if err := store.SoftDelete(ctx, id); err != nil {
			return res, fmt.Errorf("soft delete %s: %w", id, err)
		}
		res.Deleted++
	}
	return res, nil
}
