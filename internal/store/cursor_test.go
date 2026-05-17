package store

import (
	"testing"
	"time"
)

func TestEncodeDecodeCursor_RoundTrips(t *testing.T) {
	ts := time.Date(2026, 5, 17, 12, 0, 0, 123, time.UTC)
	a := &Audiobook{ID: "abc123", Title: "The Hobbit", Author: "Tolkien", CreatedAt: ts, UpdatedAt: ts}
	for _, sort := range []string{"", "title", "author", "added", "updated"} {
		cur := EncodeCursor(a, sort)
		v, id, ok := decodeCursor(cur)
		if !ok {
			t.Fatalf("sort %q: decode failed for %q", sort, cur)
		}
		if id != "abc123" {
			t.Errorf("sort %q: id = %q, want abc123", sort, id)
		}
		if v != sortValueOf(a, sort) {
			t.Errorf("sort %q: val = %q, want %q", sort, v, sortValueOf(a, sort))
		}
	}
}

// Legacy cursors were a bare id; garbage/empty must decode to "not a cursor"
// (start from page 1) rather than error or be misread as a sort value.
func TestDecodeCursor_RejectsLegacyAndGarbage(t *testing.T) {
	for _, s := range []string{
		"",                                 // empty
		"6f1b9c2d3e4f5a6b7c8d9e0f1a2b3c4d", // legacy bare hex id (no separator)
		"!!!not-base64!!!",                 // not base64
	} {
		if _, _, ok := decodeCursor(s); ok {
			t.Errorf("decodeCursor(%q) ok=true, want false", s)
		}
	}
}

func TestKeysetClause_AscDescAndParams(t *testing.T) {
	a := &Audiobook{ID: "id2", Title: "B"}
	cur := EncodeCursor(a, "title")

	var args []any
	asc := keysetClause(cur, "title", "ASC", false, &args)
	if asc != "(title > $1 OR (title = $1 AND id > $2))" {
		t.Errorf("ASC clause = %q", asc)
	}
	if len(args) != 2 || args[0] != "B" || args[1] != "id2" {
		t.Errorf("args = %v", args)
	}

	var args2 []any
	desc := keysetClause(cur, "title", "DESC", false, &args2)
	if desc != "(title < $1 OR (title = $1 AND id > $2))" {
		t.Errorf("DESC clause = %q", desc)
	}

	var args3 []any
	tm := keysetClause(EncodeCursor(a, "added"), "created_at", "ASC", true, &args3)
	if tm != "(created_at > $1::timestamptz OR (created_at = $1::timestamptz AND id > $2))" {
		t.Errorf("time clause = %q", tm)
	}

	// No/garbage cursor -> empty clause, no args appended.
	var args4 []any
	if c := keysetClause("", "title", "ASC", false, &args4); c != "" || len(args4) != 0 {
		t.Errorf("empty cursor: clause=%q args=%v", c, args4)
	}
}
