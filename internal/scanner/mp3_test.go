package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/scanner"
)

func fixtureMP3(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture %s missing: %v (regenerate with scripts/gen-m4b-fixture.sh)", p, err)
	}
	return p
}

func TestParseMP3_Tags(t *testing.T) {
	p := fixtureMP3(t, "minimal.mp3")
	got, err := scanner.ParseMP3(p)
	if err != nil {
		t.Fatalf("ParseMP3: %v", err)
	}
	if got.Title == "" {
		t.Errorf("title empty")
	}
	if got.Author == "" {
		t.Errorf("author empty")
	}
}

func TestParseMP3_SynthesizesOneChapter(t *testing.T) {
	p := fixtureMP3(t, "minimal.mp3")
	got, err := scanner.ParseMP3(p)
	if err != nil {
		t.Fatalf("ParseMP3: %v", err)
	}
	if len(got.Chapters) != 1 {
		t.Fatalf("expected 1 synthesized chapter, got %d", len(got.Chapters))
	}
}

func TestParseMP3_SidecarCoverFallback(t *testing.T) {
	dir := t.TempDir()
	src := fixtureMP3(t, "minimal.mp3")
	dst := filepath.Join(dir, "book.mp3")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	side := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), side, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	got, err := scanner.ParseMP3(dst)
	if err != nil {
		t.Fatalf("ParseMP3: %v", err)
	}
	if got.CoverMIME != "image/jpeg" {
		t.Fatalf("expected sidecar JPEG cover, mime = %q", got.CoverMIME)
	}
	if got.CoverSource != "sidecar" {
		t.Fatalf("expected CoverSource=sidecar, got %q", got.CoverSource)
	}
}
