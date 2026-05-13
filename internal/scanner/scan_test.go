package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/scanner"
)

// scanFakeStore is a minimal in-memory test double for the scanner's store
// dependency. Real store tests live in internal/store/store_test.go.
type scanFakeStore struct {
	books    map[string]scanner.Audiobook
	chapters map[string][]scanner.ParsedChapter
	covers   map[string]scanner.Cover
	deletes  map[string]bool
}

func (f *scanFakeStore) ListPaths(_ context.Context, libID int64) (map[string]string, error) {
	out := map[string]string{}
	for id, b := range f.books {
		if b.LibraryPathID == libID {
			out[id] = b.Path
		}
	}
	return out, nil
}

func (f *scanFakeStore) Upsert(_ context.Context, a scanner.Audiobook) error {
	if f.books == nil {
		f.books = map[string]scanner.Audiobook{}
	}
	f.books[a.ID] = a
	return nil
}

func (f *scanFakeStore) ReplaceChapters(_ context.Context, id string, chs []scanner.ParsedChapter) error {
	if f.chapters == nil {
		f.chapters = map[string][]scanner.ParsedChapter{}
	}
	f.chapters[id] = chs
	return nil
}

func (f *scanFakeStore) UpsertCover(_ context.Context, c scanner.Cover) error {
	if f.covers == nil {
		f.covers = map[string]scanner.Cover{}
	}
	f.covers[c.AudiobookID] = c
	return nil
}

func (f *scanFakeStore) SoftDelete(_ context.Context, id string) error {
	if f.deletes == nil {
		f.deletes = map[string]bool{}
	}
	f.deletes[id] = true
	return nil
}

func TestScan_InitialWalkInsertsBothFormats(t *testing.T) {
	dir := t.TempDir()
	m4bSrc := fixtureM4B(t, "minimal.m4b")
	mp3Src := fixtureMP3(t, "minimal.mp3")

	// Two M4Bs and one MP3 — all should be picked up.
	if err := copyFile(m4bSrc, filepath.Join(dir, "a.m4b")); err != nil {
		t.Fatalf("copy a.m4b: %v", err)
	}
	if err := copyFile(m4bSrc, filepath.Join(dir, "b.m4b")); err != nil {
		t.Fatalf("copy b.m4b: %v", err)
	}
	if err := copyFile(mp3Src, filepath.Join(dir, "c.mp3")); err != nil {
		t.Fatalf("copy c.mp3: %v", err)
	}
	// Non-audio file that must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	fake := &scanFakeStore{}
	res, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{
		LibraryPathID: 1,
		Root:          dir,
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Added != 3 || res.Changed != 0 || res.Deleted != 0 {
		t.Fatalf("counts = (added=%d changed=%d deleted=%d), want (3,0,0)", res.Added, res.Changed, res.Deleted)
	}
	if len(fake.books) != 3 {
		t.Errorf("books in fake = %d, want 3", len(fake.books))
	}
}
