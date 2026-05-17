package store

import (
	"context"
	"fmt"
	"time"
)

// Audiobook is one row in the audiobook table.
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
	Deleted       bool
	DeletedAt     *time.Time
	ScannedAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const audiobookCols = `id, library_path_id, path, file_size, mtime,
    title, author, narrator, album, year, genre, isbn, asin, description,
    duration_ms, deleted, deleted_at, scanned_at, created_at, updated_at`

func scanAudiobook(row interface {
	Scan(dest ...any) error
}) (*Audiobook, error) {
	a := &Audiobook{}
	err := row.Scan(&a.ID, &a.LibraryPathID, &a.Path, &a.FileSize, &a.MTime,
		&a.Title, &a.Author, &a.Narrator, &a.Album, &a.Year, &a.Genre, &a.ISBN, &a.ASIN, &a.Description,
		&a.DurationMs, &a.Deleted, &a.DeletedAt, &a.ScannedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// UpsertAudiobook inserts or replaces a row by id. Bumps updated_at.
func (s *Store) UpsertAudiobook(ctx context.Context, a *Audiobook) error {
	const q = `
INSERT INTO audiobook (id, library_path_id, path, file_size, mtime,
    title, author, narrator, album, year, genre, isbn, asin, description,
    duration_ms, deleted, deleted_at, scanned_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (id) DO UPDATE SET
    library_path_id = EXCLUDED.library_path_id,
    path = EXCLUDED.path,
    file_size = EXCLUDED.file_size,
    mtime = EXCLUDED.mtime,
    title = EXCLUDED.title,
    author = EXCLUDED.author,
    narrator = EXCLUDED.narrator,
    album = EXCLUDED.album,
    year = EXCLUDED.year,
    genre = EXCLUDED.genre,
    isbn = EXCLUDED.isbn,
    asin = EXCLUDED.asin,
    description = EXCLUDED.description,
    duration_ms = EXCLUDED.duration_ms,
    deleted = FALSE,
    deleted_at = NULL,
    scanned_at = EXCLUDED.scanned_at,
    updated_at = now()`
	_, err := s.pool.Exec(ctx, q,
		a.ID, a.LibraryPathID, a.Path, a.FileSize, a.MTime,
		a.Title, a.Author, a.Narrator, a.Album, a.Year, a.Genre, a.ISBN, a.ASIN, a.Description,
		a.DurationMs, a.Deleted, a.DeletedAt, a.ScannedAt)
	if err != nil {
		return fmt.Errorf("store.UpsertAudiobook: %w", err)
	}
	return nil
}

// GetAudiobook returns a single row by id.
func (s *Store) GetAudiobook(ctx context.Context, id string) (*Audiobook, error) {
	q := `SELECT ` + audiobookCols + ` FROM audiobook WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	a, err := scanAudiobook(row)
	if err != nil {
		return nil, fmt.Errorf("store.GetAudiobook: %w", err)
	}
	return a, nil
}

// ListAudiobookPathsByLibrary returns (id, path) tuples for active books in a
// library_path. Used by the scanner to detect deletions.
func (s *Store) ListAudiobookPathsByLibrary(ctx context.Context, libraryPathID int64) (map[string]string, error) {
	const q = `SELECT id, path FROM audiobook WHERE library_path_id = $1 AND deleted = FALSE`
	rows, err := s.pool.Query(ctx, q, libraryPathID)
	if err != nil {
		return nil, fmt.Errorf("store.ListAudiobookPathsByLibrary: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		out[id] = path
	}
	return out, rows.Err()
}

// SoftDeleteAudiobook marks a row deleted = true, deleted_at = now().
func (s *Store) SoftDeleteAudiobook(ctx context.Context, id string) error {
	const q = `UPDATE audiobook SET deleted = TRUE, deleted_at = now(), updated_at = now() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("store.SoftDeleteAudiobook: %w", err)
	}
	return nil
}

// FacetCount is one row from ListAuthorsWithCounts / ListGenresWithCounts.
type FacetCount struct {
	Value string
	Count int64
}

// ListAudiobooksParams is the shape for cursor-paged listing.
type ListAudiobooksParams struct {
	Cursor        string
	Limit         int
	Sort          string // "title" | "author" | "added" | "updated" — title default
	Order         string // "asc" | "desc" — asc default
	LibraryPathID int64
}

// ListActiveAudiobooks returns a cursor-paged window of non-deleted books.
// Cursor is the last-seen id; empty cursor starts at the beginning.
func (s *Store) ListActiveAudiobooks(ctx context.Context, p ListAudiobooksParams) ([]*Audiobook, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	col, isTime := resolveSort(p.Sort)
	dir := "ASC"
	if p.Order == "desc" {
		dir = "DESC"
	}
	args := []any{}
	// Exclude audiobooks whose owning library path is disabled: an operator
	// disabling a path must hide its content from catalog/search/facets.
	whereClauses := []string{"deleted = FALSE", "library_path_id IN (SELECT id FROM library_path WHERE enabled)"}
	if c := keysetClause(p.Cursor, col, dir, isTime, &args); c != "" {
		whereClauses = append(whereClauses, c)
	}
	if p.LibraryPathID > 0 {
		args = append(args, p.LibraryPathID)
		whereClauses = append(whereClauses, fmt.Sprintf("library_path_id = $%d", len(args)))
	}
	whereSQL := ""
	for i, clause := range whereClauses {
		if i > 0 {
			whereSQL += " AND "
		}
		whereSQL += clause
	}
	args = append(args, p.Limit)
	q := fmt.Sprintf(`SELECT %s FROM audiobook
WHERE %s
ORDER BY %s %s, id
LIMIT $%d`, audiobookCols, whereSQL, col, dir, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.ListActiveAudiobooks: %w", err)
	}
	defer rows.Close()
	var out []*Audiobook
	for rows.Next() {
		a, err := scanAudiobook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SearchAudiobooks runs a case-insensitive substring match across title +
// author. Same cursor shape as ListActiveAudiobooks. For v1 plain ILIKE;
// can move to a tsvector index later.
func (s *Store) SearchAudiobooks(ctx context.Context, query string, p ListAudiobooksParams) ([]*Audiobook, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	col, isTime := resolveSort(p.Sort)
	dir := "ASC"
	if p.Order == "desc" {
		dir = "DESC"
	}
	pattern := "%" + query + "%"
	args := []any{pattern}
	whereClauses := []string{"deleted = FALSE", "library_path_id IN (SELECT id FROM library_path WHERE enabled)", "(title ILIKE $1 OR author ILIKE $1)"}
	if c := keysetClause(p.Cursor, col, dir, isTime, &args); c != "" {
		whereClauses = append(whereClauses, c)
	}
	if p.LibraryPathID > 0 {
		args = append(args, p.LibraryPathID)
		whereClauses = append(whereClauses, fmt.Sprintf("library_path_id = $%d", len(args)))
	}
	whereSQL := ""
	for i, clause := range whereClauses {
		if i > 0 {
			whereSQL += " AND "
		}
		whereSQL += clause
	}
	args = append(args, p.Limit)
	q := fmt.Sprintf(`SELECT %s FROM audiobook
WHERE %s
ORDER BY %s %s, id
LIMIT $%d`, audiobookCols, whereSQL, col, dir, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.SearchAudiobooks: %w", err)
	}
	defer rows.Close()
	var out []*Audiobook
	for rows.Next() {
		a, err := scanAudiobook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAuthorsWithCounts returns distinct non-empty authors with their book
// counts, cursor-paged. Cursor is the last-seen author name (alphabetical).
func (s *Store) ListAuthorsWithCounts(ctx context.Context, cursor string, limit int, libraryPathID int64) ([]FacetCount, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{}
	whereClauses := []string{"deleted = FALSE", "library_path_id IN (SELECT id FROM library_path WHERE enabled)", "author <> ''"}
	if cursor != "" {
		args = append(args, cursor)
		whereClauses = append(whereClauses, fmt.Sprintf("author > $%d", len(args)))
	}
	if libraryPathID > 0 {
		args = append(args, libraryPathID)
		whereClauses = append(whereClauses, fmt.Sprintf("library_path_id = $%d", len(args)))
	}
	whereSQL := ""
	for i, clause := range whereClauses {
		if i > 0 {
			whereSQL += " AND "
		}
		whereSQL += clause
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT author, COUNT(*) FROM audiobook
WHERE %s
GROUP BY author
ORDER BY author
LIMIT $%d`, whereSQL, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.ListAuthorsWithCounts: %w", err)
	}
	defer rows.Close()
	var out []FacetCount
	for rows.Next() {
		var f FacetCount
		if err := rows.Scan(&f.Value, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListGenresWithCounts mirrors ListAuthorsWithCounts for the genre column.
func (s *Store) ListGenresWithCounts(ctx context.Context, cursor string, limit int, libraryPathID int64) ([]FacetCount, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{}
	whereClauses := []string{"deleted = FALSE", "library_path_id IN (SELECT id FROM library_path WHERE enabled)", "genre <> ''"}
	if cursor != "" {
		args = append(args, cursor)
		whereClauses = append(whereClauses, fmt.Sprintf("genre > $%d", len(args)))
	}
	if libraryPathID > 0 {
		args = append(args, libraryPathID)
		whereClauses = append(whereClauses, fmt.Sprintf("library_path_id = $%d", len(args)))
	}
	whereSQL := ""
	for i, clause := range whereClauses {
		if i > 0 {
			whereSQL += " AND "
		}
		whereSQL += clause
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT genre, COUNT(*) FROM audiobook
WHERE %s
GROUP BY genre
ORDER BY genre
LIMIT $%d`, whereSQL, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store.ListGenresWithCounts: %w", err)
	}
	defer rows.Close()
	var out []FacetCount
	for rows.Next() {
		var f FacetCount
		if err := rows.Scan(&f.Value, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
