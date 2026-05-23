package scanner

import (
	"context"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/store"
)

// StoreAdapter wraps *store.Store to satisfy ScanStore. Lives in the
// scanner package (not store) so tests can use the in-memory fake without
// touching the real store.
type StoreAdapter struct {
	S *store.Store
}

func (a *StoreAdapter) ListRefs(ctx context.Context, libraryPathID int64) (map[string]PathRef, error) {
	refs, err := a.S.ListAudiobookRefsByLibrary(ctx, libraryPathID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PathRef, len(refs))
	for path, r := range refs {
		out[path] = PathRef{ID: r.ID, ContentSig: r.ContentSig}
	}
	return out, nil
}

func (a *StoreAdapter) Upsert(ctx context.Context, b Audiobook) error {
	return a.S.UpsertAudiobook(ctx, &store.Audiobook{
		ID:            b.ID,
		LibraryPathID: b.LibraryPathID,
		Path:          b.Path,
		FileSize:      b.FileSize,
		MTime:         b.MTime,
		Title:         b.Title,
		Author:        b.Author,
		Narrator:      b.Narrator,
		Album:         b.Album,
		Year:          b.Year,
		Genre:         b.Genre,
		ISBN:          b.ISBN,
		ASIN:          b.ASIN,
		Description:   b.Description,
		DurationMs:    b.DurationMs,
		ScannedAt:     b.ScannedAt,
		ContentSig:    b.ContentSig,
	})
}

func (a *StoreAdapter) ReplaceChapters(ctx context.Context, audiobookID string, chs []ParsedChapter) error {
	out := make([]store.Chapter, len(chs))
	for i, c := range chs {
		out[i] = store.Chapter{
			AudiobookID: audiobookID,
			Idx:         c.Idx,
			Title:       c.Title,
			StartMs:     c.StartMs,
			EndMs:       c.EndMs,
		}
	}
	return a.S.ReplaceChapters(ctx, audiobookID, out)
}

func (a *StoreAdapter) UpsertCover(ctx context.Context, c Cover) error {
	return a.S.UpsertCover(ctx, store.Cover{
		AudiobookID: c.AudiobookID,
		ContentType: c.ContentType,
		Bytes:       c.Bytes,
		Source:      c.Source,
	})
}

func (a *StoreAdapter) SoftDelete(ctx context.Context, audiobookID string) error {
	return a.S.SoftDeleteAudiobook(ctx, audiobookID)
}

// Compile-time check.
var _ ScanStore = (*StoreAdapter)(nil)
