package scanner

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// ParseMP3 extracts tags, duration, cover, and a synthesized single
// chapter from a single-file MP3 audiobook. Mirrors ParseM4B's shape so
// callers can treat both formats uniformly. The plugin never modifies the
// MP3 file.
//
// Duration comes from the ID3v2 TLEN frame (milliseconds). If TLEN is
// absent the duration stays 0 — the synthesized chapter will have
// EndMs=0, the file still plays fine, and a future task can add MP3 frame
// scanning for duration when this becomes a problem.
//
// Cover priority: APIC frame embedded in the ID3 tag, then sidecar
// (cover.jpg / cover.png / folder.jpg) via the shared readSidecarCover
// helper. CoverSource is "embedded" or "sidecar" so the caller can record
// the provenance in the cover.source DB column.
//
// Chapters: ID3v2 CHAP frame parsing is a documented follow-up. v1
// synthesizes a single chapter spanning the duration (matches M4B
// behavior for chapterless files).
func ParseMP3(path string) (*ParsedM4B, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	out := &ParsedM4B{}

	m, err := tag.ReadFrom(f)
	if err == nil {
		out.Title = m.Title()
		out.Album = m.Album()
		out.Genre = m.Genre()
		if y := m.Year(); y > 0 {
			out.Year = fmt.Sprintf("%d", y)
		}
		out.Description = m.Comment()
		albumArtist := m.AlbumArtist()
		artist := m.Artist()
		switch {
		case albumArtist != "" && artist != "" && albumArtist != artist:
			out.Author = albumArtist
			out.Narrator = artist
		case albumArtist != "":
			out.Author = albumArtist
			out.Narrator = artist
		case artist != "":
			out.Author = artist
		}

		raw := m.Raw()
		if v, ok := raw["asin"].(string); ok {
			out.ASIN = v
		}
		if v, ok := raw["isbn"].(string); ok {
			out.ISBN = v
		}

		// Duration: ID3v2 TLEN frame holds milliseconds as ASCII. Try the
		// canonical key first, then a lowercase variant; some producers
		// emit oddly-cased frame IDs.
		out.DurationMs = parseTLEN(raw)

		// Cover: APIC frame via Picture().
		if pic := m.Picture(); pic != nil && len(pic.Data) > 0 {
			out.CoverBytes = pic.Data
			out.CoverMIME = pic.MIMEType
			out.CoverSource = "embedded"
		}
	}

	// Sidecar cover fallback shared with ParseM4B.
	if len(out.CoverBytes) == 0 {
		if b, mime := readSidecarCover(path); len(b) > 0 {
			out.CoverBytes, out.CoverMIME = b, mime
			out.CoverSource = "sidecar"
		}
	}

	// Synthesize a single chapter spanning the duration. End is 0 when
	// duration is unknown; the chapter still represents "the whole book".
	out.Chapters = []ParsedChapter{{
		Idx:     0,
		Title:   "Chapter 1",
		StartMs: 0,
		EndMs:   out.DurationMs,
	}}

	return out, nil
}

// parseTLEN reads the duration in milliseconds from an ID3v2 TLEN frame.
// dhowden/tag exposes raw frame data under the frame-id key. Returns 0
// when TLEN is missing or unparseable; callers degrade gracefully (the
// synthesized chapter just has EndMs=0).
func parseTLEN(raw map[string]any) int64 {
	keys := []string{"TLEN", "tlen"}
	for _, k := range keys {
		v, ok := raw[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}
