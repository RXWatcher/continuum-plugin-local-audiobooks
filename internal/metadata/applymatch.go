package metadata

import "strings"

// AudiobookRow is the subset of audiobook columns ApplyMatch reads/writes.
type AudiobookRow struct {
	ID          string
	Title       string
	Author      string
	Narrator    string
	Description string
	Year        string
	Genre       string
	ISBN        string
	ASIN        string
	DurationMS  int64
}

// ApplyMatch returns a new AudiobookRow with `candidate`'s fields overwriting
// `row`'s, preserving non-empty existing fields when the candidate is empty,
// and preserving the existing ID always.
func ApplyMatch(row AudiobookRow, candidate Candidate) AudiobookRow {
	out := row
	if candidate.Title != "" {
		out.Title = candidate.Title
	}
	if a := strings.Join(candidate.Authors, ", "); a != "" {
		out.Author = a
	}
	if n := strings.Join(candidate.Narrators, ", "); n != "" {
		out.Narrator = n
	}
	if candidate.Description != "" {
		out.Description = candidate.Description
	}
	if y := yearOf(candidate.PublishedAt); y != "" {
		out.Year = y
	}
	if g := strings.Join(candidate.Genres, ", "); g != "" {
		out.Genre = g
	}
	if candidate.ISBN != "" {
		out.ISBN = candidate.ISBN
	}
	if candidate.ASIN != "" {
		out.ASIN = candidate.ASIN
	}
	if candidate.RuntimeMin > 0 {
		out.DurationMS = int64(candidate.RuntimeMin) * 60_000
	}
	return out
}

func yearOf(s string) string {
	if len(s) >= 4 {
		return s[:4]
	}
	return ""
}
