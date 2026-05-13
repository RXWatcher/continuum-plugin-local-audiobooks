// Package metadata holds the audiobook metadata aggregator: source-agnostic
// Candidate/Match types, the external-ID format, the confidence formula,
// the cache + queue stores, and the parallel aggregator.
package metadata

import "encoding/json"

// Candidate is the source-agnostic representation of a single metadata
// record. Each Source returns these; the aggregator wraps them in Match.
type Candidate struct {
	Source      string          `json:"source"`
	ExternalID  string          `json:"external_id"` // native ID, no "source:" prefix
	Title       string          `json:"title"`
	Authors     []string        `json:"authors,omitempty"`
	Narrators   []string        `json:"narrators,omitempty"`
	Description string          `json:"description,omitempty"`
	ASIN        string          `json:"asin,omitempty"`
	ISBN        string          `json:"isbn,omitempty"`
	CoverURL    string          `json:"cover_url,omitempty"`
	PublishedAt string          `json:"published_at,omitempty"` // YYYY-MM-DD or YYYY
	Publisher   string          `json:"publisher,omitempty"`
	Language    string          `json:"language,omitempty"`
	Genres      []string        `json:"genres,omitempty"`
	RuntimeMin  int             `json:"runtime_min,omitempty"`
	Series      string          `json:"series,omitempty"`
	SeriesPos   string          `json:"series_pos,omitempty"`
	Chapters    []Chapter       `json:"chapters,omitempty"`
	Region      string          `json:"region,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"` // upstream payload as-is
}

// Chapter is one chapter within a Candidate.
type Chapter struct {
	Title   string `json:"title"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}

// Match wraps a Candidate with its confidence score.
type Match struct {
	Source     string
	Confidence int       // 0-100
	Candidate  Candidate
}
