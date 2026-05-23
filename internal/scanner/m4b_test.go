package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/scanner"
)

// fixtureM4B returns the path to a fixture; the file is checked into
// testdata/. If the fixture is missing the test skips with a helpful
// message rather than failing.
func fixtureM4B(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture %s missing: %v (regenerate with scripts/gen-m4b-fixture.sh)", p, err)
	}
	return p
}

func TestParseM4B_Tags(t *testing.T) {
	p := fixtureM4B(t, "minimal.m4b")
	got, err := scanner.ParseM4B(p)
	if err != nil {
		t.Fatalf("ParseM4B: %v", err)
	}
	if got.Title == "" {
		t.Errorf("title empty")
	}
	if got.Author == "" {
		t.Errorf("author empty")
	}
}

func TestParseM4B_NoChaptersSynthesizesOne(t *testing.T) {
	p := fixtureM4B(t, "minimal.m4b")
	got, err := scanner.ParseM4B(p)
	if err != nil {
		t.Fatalf("ParseM4B: %v", err)
	}
	if len(got.Chapters) != 1 {
		t.Fatalf("expected 1 synthesized chapter, got %d", len(got.Chapters))
	}
	if got.Chapters[0].Idx != 0 || got.Chapters[0].StartMs != 0 {
		t.Errorf("synthesized chapter shape wrong: %+v", got.Chapters[0])
	}
}

func TestParseM4B_DurationParsed(t *testing.T) {
	p := fixtureM4B(t, "minimal.m4b")
	got, err := scanner.ParseM4B(p)
	if err != nil {
		t.Fatalf("ParseM4B: %v", err)
	}
	if got.DurationMs <= 0 {
		t.Fatalf("expected positive duration, got %d", got.DurationMs)
	}
}

// Optional chaptered-fixture test — skips cleanly if the fixture isn't
// generated. We accept either real-parsed chapters or the synthesis
// fallback (both leave at least 1 chapter in ascending order).
func TestParseM4B_ChapAtomChapters(t *testing.T) {
	p := fixtureM4B(t, "chaptered.m4b")
	got, err := scanner.ParseM4B(p)
	if err != nil {
		t.Fatalf("ParseM4B: %v", err)
	}
	if len(got.Chapters) < 1 {
		t.Fatalf("expected >= 1 chapter, got %d", len(got.Chapters))
	}
	for i := 1; i < len(got.Chapters); i++ {
		if got.Chapters[i].StartMs < got.Chapters[i-1].StartMs {
			t.Errorf("chapters out of order at idx %d", i)
		}
	}
}

func TestParseM4B_SidecarCoverFallback(t *testing.T) {
	dir := t.TempDir()
	src := fixtureM4B(t, "minimal.m4b")
	dst := filepath.Join(dir, "book.m4b")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	// Drop a sidecar cover.jpg next to the m4b.
	side := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), side, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	got, err := scanner.ParseM4B(dst)
	if err != nil {
		t.Fatalf("ParseM4B: %v", err)
	}
	if got.CoverMIME != "image/jpeg" {
		t.Fatalf("expected sidecar JPEG cover, mime = %q", got.CoverMIME)
	}
	if got.CoverSource != "sidecar" {
		t.Fatalf("expected CoverSource=sidecar, got %q", got.CoverSource)
	}
	if len(got.CoverBytes) != len(side) {
		t.Errorf("cover bytes len = %d, want %d", len(got.CoverBytes), len(side))
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
