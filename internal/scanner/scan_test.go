package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ContinuumApp/continuum-plugin-local-audiobooks/internal/scanner"
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

func TestScan_DetectsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	src := fixtureM4B(t, "minimal.m4b")
	target := filepath.Join(dir, "a.m4b")
	if err := copyFile(src, target); err != nil {
		t.Fatalf("copy: %v", err)
	}
	fake := &scanFakeStore{}

	r1, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{LibraryPathID: 1, Root: dir})
	if err != nil {
		t.Fatalf("first walk: %v", err)
	}
	if r1.Added != 1 {
		t.Fatalf("first walk added = %d, want 1", r1.Added)
	}

	// Touch the file to bump mtime.
	future := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(target, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	r2, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{LibraryPathID: 1, Root: dir})
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if r2.Changed != 1 || r2.Added != 0 || r2.Deleted != 1 {
		t.Errorf("counts after change = (a=%d c=%d d=%d), want (0,1,1)", r2.Added, r2.Changed, r2.Deleted)
	}
}

func TestScan_SoftDeletesDisappeared(t *testing.T) {
	dir := t.TempDir()
	src := fixtureM4B(t, "minimal.m4b")
	target := filepath.Join(dir, "a.m4b")
	if err := copyFile(src, target); err != nil {
		t.Fatalf("copy: %v", err)
	}
	fake := &scanFakeStore{}

	if _, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{LibraryPathID: 1, Root: dir}); err != nil {
		t.Fatalf("first walk: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove: %v", err)
	}
	r2, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{LibraryPathID: 1, Root: dir})
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if r2.Deleted != 1 {
		t.Fatalf("expected 1 soft-delete, got %d", r2.Deleted)
	}
	if len(fake.deletes) != 1 {
		t.Errorf("fake.deletes = %v", fake.deletes)
	}
}

// fakeEnqueuer records the audiobook IDs passed to Enqueue.
type fakeEnqueuer struct{ ids []string }

func (f *fakeEnqueuer) Enqueue(_ context.Context, id string) error {
	f.ids = append(f.ids, id)
	return nil
}

func TestScan_EnqueuesEnrichmentOnInsert(t *testing.T) {
	dir := t.TempDir()
	src := fixtureM4B(t, "minimal.m4b")
	if err := copyFile(src, filepath.Join(dir, "a.m4b")); err != nil {
		t.Fatalf("copy: %v", err)
	}

	enq := &fakeEnqueuer{}
	fake := &scanFakeStore{}
	res, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{
		LibraryPathID:   1,
		Root:            dir,
		EnrichmentQueue: enq,
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("expected 1 added, got %d", res.Added)
	}
	if len(enq.ids) != 1 {
		t.Errorf("expected 1 enrichment enqueue after insert, got %d", len(enq.ids))
	}
}

func TestScan_EnqueuesEnrichmentOnUpdate(t *testing.T) {
	dir := t.TempDir()
	src := fixtureM4B(t, "minimal.m4b")
	target := filepath.Join(dir, "a.m4b")
	if err := copyFile(src, target); err != nil {
		t.Fatalf("copy: %v", err)
	}

	fake := &scanFakeStore{}
	// First walk — inserts the book.
	if _, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{LibraryPathID: 1, Root: dir}); err != nil {
		t.Fatalf("first walk: %v", err)
	}

	// Touch the file to trigger a content-changed update on the second walk.
	future := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(target, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	enq := &fakeEnqueuer{}
	r2, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{
		LibraryPathID:   1,
		Root:            dir,
		EnrichmentQueue: enq,
	})
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if r2.Changed != 1 {
		t.Fatalf("expected 1 changed, got %d", r2.Changed)
	}
	if len(enq.ids) != 1 {
		t.Errorf("expected 1 enrichment enqueue after update, got %d", len(enq.ids))
	}
}

func TestScan_NoEnqueueWhenQueueNil(t *testing.T) {
	// Existing tests don't supply EnrichmentQueue. This test confirms nil is safe.
	dir := t.TempDir()
	src := fixtureM4B(t, "minimal.m4b")
	if err := copyFile(src, filepath.Join(dir, "a.m4b")); err != nil {
		t.Fatalf("copy: %v", err)
	}
	fake := &scanFakeStore{}
	res, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{
		LibraryPathID: 1,
		Root:          dir,
		// EnrichmentQueue intentionally omitted (nil).
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("expected 1 added, got %d", res.Added)
	}
}

// One corrupt/unparseable audio file must not abort the whole library scan:
// it is logged and skipped so the rest of the library still indexes.
func TestScan_SkipsUnparseableFileWithoutAbortingScan(t *testing.T) {
	dir := t.TempDir()
	good := fixtureM4B(t, "minimal.m4b")
	if err := copyFile(good, filepath.Join(dir, "good.m4b")); err != nil {
		t.Fatalf("copy good: %v", err)
	}
	// A recognised extension whose content can't be opened/parsed (dangling
	// symlink) — must be skipped, not abort the whole scan.
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), filepath.Join(dir, "broken.m4b")); err != nil {
		t.Fatalf("symlink broken: %v", err)
	}

	fake := &scanFakeStore{}
	res, err := scanner.Walk(context.Background(), fake, scanner.WalkParams{LibraryPathID: 1, Root: dir})
	if err != nil {
		t.Fatalf("one bad file must not abort the scan; got err=%v", err)
	}
	if res.Added != 1 || len(fake.books) != 1 {
		t.Errorf("good file should still index: added=%d books=%d", res.Added, len(fake.books))
	}
}
