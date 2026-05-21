package scanner_test

import (
	"testing"
	"time"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/scanner"
)

func TestStableID_ChangesWithSize(t *testing.T) {
	mt := time.Unix(1_700_000_000, 0)
	a := scanner.StableID("/srv/foo.m4b", 12345, mt)
	b := scanner.StableID("/srv/foo.m4b", 12346, mt)
	if a == b {
		t.Fatal("expected different IDs for different sizes")
	}
}

func TestStableID_StableAcrossCalls(t *testing.T) {
	mt := time.Unix(1_700_000_000, 0)
	a := scanner.StableID("/srv/foo.m4b", 12345, mt)
	b := scanner.StableID("/srv/foo.m4b", 12345, mt)
	if a != b {
		t.Fatalf("unstable: %q vs %q", a, b)
	}
}

func TestStableID_ChangesWithMtime(t *testing.T) {
	a := scanner.StableID("/srv/foo.m4b", 12345, time.Unix(1_700_000_000, 0))
	b := scanner.StableID("/srv/foo.m4b", 12345, time.Unix(1_700_000_001, 0))
	if a == b {
		t.Fatal("expected different IDs for different mtimes")
	}
}

func TestStableID_HexLength(t *testing.T) {
	id := scanner.StableID("/x", 1, time.Unix(0, 0))
	if len(id) != 32 {
		t.Fatalf("expected 32 hex chars (128 bits), got %d (%q)", len(id), id)
	}
}
