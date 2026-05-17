package store

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Catalog/search pages are ordered by `<sortcol> <dir>, id ASC`. A cursor
// must therefore carry BOTH the sort value and the id of the last row so the
// next page resumes exactly. The previous implementation cursored on id only
// while ordering by the sort column, which skipped and duplicated rows.

const (
	cursorSep = "\x1f" // unit separator — never appears in titles or hex ids
	// cursorMagic prefixes every composite cursor so a legacy bare-id cursor
	// (or any garbage) is reliably rejected: arbitrary base64-decoded bytes
	// have a ~1/2^40 chance of starting with exactly this, not the ~1/256 of
	// a single version byte.
	cursorMagic = "lab1"
)

// resolveSort maps the API sort param to its SQL column and whether that
// column is a timestamp (so a text cursor param needs a ::timestamptz cast).
func resolveSort(sort string) (col string, isTime bool) {
	switch sort {
	case "author":
		return "author", false
	case "added":
		return "created_at", true
	case "updated":
		return "updated_at", true
	default:
		return "title", false
	}
}

func sortValueOf(a *Audiobook, sort string) string {
	switch sort {
	case "author":
		return a.Author
	case "added":
		return a.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updated":
		return a.UpdatedAt.UTC().Format(time.RFC3339Nano)
	default:
		return a.Title
	}
}

// EncodeCursor builds the opaque next-page cursor from the last row of a page
// for the given sort.
func EncodeCursor(a *Audiobook, sort string) string {
	raw := cursorMagic + cursorSep + sortValueOf(a, sort) + cursorSep + a.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (val, id string, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", "", false
	}
	rest, found := strings.CutPrefix(string(b), cursorMagic+cursorSep)
	if !found {
		return "", "", false
	}
	// Split on the LAST separator: ids never contain it, titles realistically
	// never do, but this is robust even if one did.
	i := strings.LastIndex(rest, cursorSep)
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// keysetClause appends the composite (sortvalue,id) keyset predicate for the
// page after `cursor` to args and returns the SQL fragment, or "" when the
// cursor is absent/legacy/garbage (in which case the caller starts from the
// first page rather than erroring). Ordering is `<col> <dir>, id ASC`.
func keysetClause(cursor, col, dir string, isTime bool, args *[]any) string {
	v, id, ok := decodeCursor(cursor)
	if !ok {
		return ""
	}
	*args = append(*args, v)
	vRef := fmt.Sprintf("$%d", len(*args))
	if isTime {
		vRef += "::timestamptz"
	}
	*args = append(*args, id)
	idRef := fmt.Sprintf("$%d", len(*args))
	cmp := ">"
	if dir == "DESC" {
		cmp = "<"
	}
	// (col <cmp> v) OR (col = v AND id > id) — matches ORDER BY col dir, id ASC.
	return fmt.Sprintf("(%s %s %s OR (%s = %s AND id > %s))", col, cmp, vRef, col, vRef, idRef)
}
