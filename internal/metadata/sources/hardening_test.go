package sources

import (
	"sync"
	"testing"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata"
)

func TestIsCCTLD(t *testing.T) {
	ok := []string{"se", "no", "uk", "com", "dk"}
	bad := []string{"", "x", "abcd", "com.attacker.net", "com/..", "S E", "c0m", "..", "/"}
	for _, s := range ok {
		if !isCCTLD(s) {
			t.Errorf("isCCTLD(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if isCCTLD(s) {
			t.Errorf("isCCTLD(%q) = true, want false", s)
		}
	}
}

// A region that isn't a bare ccTLD must NOT be interpolated into the host
// (SSRF) — it falls back to the global .com site.
func TestStorytelHostFor_RejectsSSRFRegion(t *testing.T) {
	s := NewStorytel("ua")
	cases := map[string]string{
		"":                 "https://www.storytel.com",
		"us":               "https://www.storytel.com",
		"uk":               "https://www.storytel.co.uk",
		"se":               "https://www.storytel.se",
		"com.attacker.net": "https://www.storytel.com",
		"com/../../":       "https://www.storytel.com",
		"169.254.169.254":  "https://www.storytel.com",
		"evil\nhost":       "https://www.storytel.com",
	}
	for region, want := range cases {
		if got := s.storytelHostFor(region); got != want {
			t.Errorf("storytelHostFor(%q) = %q, want %q", region, got, want)
		}
	}
}

// A pathologically deep scraped JSON tree must not exhaust the stack: the
// depth guard makes traversal return instead of recursing unbounded.
func TestTraverseNextData_DepthBounded(t *testing.T) {
	var v interface{} = map[string]interface{}{"leaf": "x"}
	for i := 0; i < 100000; i++ {
		v = []interface{}{v}
	}
	done := make(chan struct{})
	go func() {
		var out []metadata.Candidate
		traverseNextData(v, &out, 0) // must return, not blow the stack
		close(done)
	}()
	<-done
}

// Registry is read concurrently by gRPC handlers / the worker while a
// reconfigure may Register; guarded access must be race-free (go test -race).
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); r.Register(NewStorytel("ua")) }()
		go func() { defer wg.Done(); _ = r.ForID("storytel"); _ = r.All() }()
	}
	wg.Wait()
}
