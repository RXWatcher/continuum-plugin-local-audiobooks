package store

import (
	"context"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata"
)

// UpdateAudiobookMetadata writes metadata fields onto an audiobook row.
func (s *Store) UpdateAudiobookMetadata(ctx context.Context, row metadata.AudiobookRow) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE audiobook SET
		  title = $2, author = $3, narrator = $4, description = $5,
		  year = $6, genre = $7, isbn = $8, asin = $9, duration_ms = $10,
		  updated_at = now()
		WHERE id = $1
	`,
		row.ID, row.Title, row.Author, row.Narrator, row.Description,
		row.Year, row.Genre, row.ISBN, row.ASIN, row.DurationMS,
	)
	return err
}

// LoadAudiobookRow returns the audiobook fields ApplyMatch operates on.
func (s *Store) LoadAudiobookRow(ctx context.Context, id string) (metadata.AudiobookRow, error) {
	var r metadata.AudiobookRow
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, author, narrator, description, year, genre, isbn, asin, duration_ms
		FROM audiobook WHERE id = $1
	`, id).Scan(&r.ID, &r.Title, &r.Author, &r.Narrator, &r.Description,
		&r.Year, &r.Genre, &r.ISBN, &r.ASIN, &r.DurationMS)
	return r, err
}

// ListPendingBackfill returns IDs of audiobooks that have no enrichment job yet.
func (s *Store) ListPendingBackfill(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id FROM audiobook a
		LEFT JOIN metadata_enrichment_job j ON j.audiobook_id = a.id
		WHERE a.deleted = FALSE AND j.audiobook_id IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
